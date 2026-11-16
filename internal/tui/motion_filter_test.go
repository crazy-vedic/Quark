package tui

import (
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/crazy-vedic/quark/internal/domain"
)

type sink struct {
	mu   sync.Mutex
	msgs []tea.Msg
}

func (s *sink) send(m tea.Msg) { s.mu.Lock(); s.msgs = append(s.msgs, m); s.mu.Unlock() }
func (s *sink) all() []tea.Msg {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]tea.Msg(nil), s.msgs...)
}

func TestMotionCoalescerHoldsBurstAndDeliversNewest(t *testing.T) {
	c := NewMotionCoalescer()
	out := &sink{}
	c.Bind(out.send)

	assert.NotNil(t, c.Filter(nil, dragTo(1, 1)), "the first motion passes at once")
	for x := 2; x <= 50; x++ {
		assert.Nil(t, c.Filter(nil, dragTo(x, 1)), "motion inside the window is held")
	}
	require.Eventually(t, func() bool { return len(out.all()) == 1 }, time.Second, time.Millisecond)
	time.Sleep(3 * motionWindow)
	got := out.all()
	require.Len(t, got, 1, "a burst delivers one event, not one per cell")
	held, ok := got[0].(coalescedMotion)
	require.True(t, ok)
	assert.Equal(t, 50, held.X, "the newest position wins")

	// The held event re-enters the filter and is unwrapped, not held again.
	back, ok := c.Filter(nil, got[0]).(tea.MouseMsg)
	require.True(t, ok)
	assert.Equal(t, 50, back.X)
}

func TestMotionCoalescerReleaseAndOtherMessagesPassThrough(t *testing.T) {
	c := NewMotionCoalescer()
	out := &sink{}
	c.Bind(out.send)
	c.Filter(nil, dragTo(1, 1))
	assert.Nil(t, c.Filter(nil, dragTo(9, 1)))

	release := releaseAt(9, 1)
	assert.Equal(t, release, c.Filter(nil, release))
	time.Sleep(3 * motionWindow)
	assert.Empty(t, out.all(), "a release replaces the held motion; nothing is sent late")

	key := tea.KeyMsg{Type: tea.KeyEnter}
	assert.Equal(t, key, c.Filter(nil, key))
}

func TestMotionCoalescerWithoutSendNeverHolds(t *testing.T) {
	c := NewMotionCoalescer()
	c.Filter(nil, dragTo(1, 1))
	assert.NotNil(t, c.Filter(nil, dragTo(2, 1)), "an unbound filter must not swallow events")
}

// The release lands on the pointer's final cell and finishes the selection.
func TestReleaseExtendsSelectionToFinalCell(t *testing.T) {
	m := editorTestModel(t, nil)
	x, y, ok := m.RequestURLLineClickPos()
	require.True(t, ok)
	updated, _ := m.Update(press(x, y))
	m = requireModel(t, updated)
	start := m.urlInput.Position()
	m.lastTextClickSource = ""
	release := releaseAt(x+7, y)
	updated, _ = m.Update(release)
	m = requireModel(t, updated)
	assert.Equal(t, start, m.urlSelection.Anchor)
	assert.Equal(t, start+7, m.urlSelection.Cursor)

	// A plain click's release at the same cell selects nothing.
	m2 := editorTestModel(t, nil)
	updated, _ = m2.Update(press(x, y))
	m2 = requireModel(t, updated)
	updated, _ = m2.Update(releaseAt(x, y))
	assert.False(t, requireModel(t, updated).urlSelection.Active())
}

func releaseAt(x, y int) tea.MouseMsg {
	return tea.MouseMsg{X: x, Y: y, Action: tea.MouseActionRelease, Button: tea.MouseButtonLeft}
}

func benchBody(lines int) string {
	rows := make([]string, lines)
	for i := range rows {
		rows[i] = fmt.Sprintf(`  "field_%03d": "%s",`, i, strings.Repeat("v", 30+i%20))
	}
	return "{\n" + strings.Join(rows, "\n") + "\n}"
}

// BenchmarkBodyDragMotion measures one drag-motion event on a large body, with
// the frame rendered before each event as Bubble Tea does.
func BenchmarkBodyDragMotion(b *testing.B) {
	for _, lines := range []int{20, 200, 2000} {
		b.Run(fmt.Sprintf("lines=%d", lines), func(b *testing.B) {
			req := &domain.Request{ID: "r", Method: "POST", URL: "u", Body: benchBody(lines)}
			m := editorTestModel(&testing.T{}, req)
			m = m.WithActiveField(bodyField).WithBodyEdit()
			layout := m.currentLayout()
			y := m.requestPaneLineLayout(layout).editorContentY
			left := m.bodyTextareaTextLeft(layout)
			_ = m.View() // the click handler reuses this frame
			m = m.handleBodyTextareaClick(press(left+3, y))
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				mm := dragTo(left+5+i%20, y+i%8)
				mm.Shift = true
				m = m.handleBodyTextareaClick(mm)
			}
		})
	}
}
