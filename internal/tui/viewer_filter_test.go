package tui

import (
	"context"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/crazy-vedic/quark/internal/exec"
)

// jsonViewerTestModel opens the response viewer on body.
func jsonViewerTestModel(t *testing.T, body string) Model {
	t.Helper()
	m := New(Deps{})
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m = requireModel(t, updated)
	m = m.WithResponse(&exec.ExecuteResult{
		StatusCode: 200,
		Status:     "200 OK",
		Body:       []byte(body),
	})
	m = m.openViewerForResponse()
	require.Equal(t, viewerMode, m.mode)
	return m
}

// typeViewerFilter opens the filter with | and types text into it. The
// returned command is the one the last keypress produced.
func typeViewerFilter(t *testing.T, m Model, text string) (Model, tea.Cmd) {
	t.Helper()
	m, _ = sendKey(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'|'}})
	require.True(t, m.viewerFilter.open)
	var cmd tea.Cmd
	for _, r := range text {
		m, cmd = sendKey(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	return m, cmd
}

// deliverViewerFilterResults runs cmd and feeds each filter result it yields
// back into Update, as the Bubble Tea loop would. Other messages are dropped.
func deliverViewerFilterResults(t *testing.T, m Model, cmd tea.Cmd) Model {
	t.Helper()
	require.NotNil(t, cmd)
	var msgs []tea.Msg
	if batch, ok := cmd().(tea.BatchMsg); ok {
		for _, c := range batch {
			if c != nil {
				msgs = append(msgs, c())
			}
		}
	} else {
		msgs = append(msgs, cmd())
	}
	for _, msg := range msgs {
		if result, ok := msg.(viewerFilterResultMsg); ok {
			m, _ = sendMsg(t, m, result)
		}
	}
	return m
}

func sendMsg(t *testing.T, m Model, msg tea.Msg) (Model, tea.Cmd) {
	t.Helper()
	updated, cmd := m.Update(msg)
	return requireModel(t, updated), cmd
}

const filterTestBody = `{"items":[{"id":1,"name":"a"},{"id":2,"name":"b"}],"total":2}`

func TestViewerFilterPipeKeyOpensInput(t *testing.T) {
	m := jsonViewerTestModel(t, filterTestBody)
	m, _ = sendKey(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'|'}})
	assert.True(t, m.viewerFilter.open)
	assert.Equal(t, viewerMode, m.mode)
	assert.Contains(t, stripANSI(m.viewViewer()), "jq: ")
}

func TestViewerFilterShowsResultWhileTyping(t *testing.T) {
	m := jsonViewerTestModel(t, filterTestBody)
	m, cmd := typeViewerFilter(t, m, ".items[].id")
	assert.True(t, m.viewerFilter.pending, "evaluation runs in a command, not in Update")
	require.NotNil(t, cmd)

	m = deliverViewerFilterResults(t, m, cmd)
	assert.False(t, m.viewerFilter.pending)
	assert.True(t, m.viewerFilter.active)
	assert.Empty(t, m.viewerFilter.err)
	assert.Equal(t, "1\n2", m.viewerCopy)
	assert.Equal(t, "1\n2", stripANSI(m.viewerContent))
}

func TestViewerFilterReportsErrorAndKeepsLastResult(t *testing.T) {
	m := jsonViewerTestModel(t, filterTestBody)
	m, cmd := typeViewerFilter(t, m, ".items[].id")
	m = deliverViewerFilterResults(t, m, cmd)

	m, cmd = sendKey(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'['}})
	m = deliverViewerFilterResults(t, m, cmd)
	assert.NotEmpty(t, m.viewerFilter.err)
	assert.Contains(t, stripANSI(m.viewViewer()), "error:")
	assert.Equal(t, "1\n2", m.viewerCopy, "the last good result stays on screen")
}

func TestViewerFilterNonJSONBodyIsAnError(t *testing.T) {
	m := viewerTestModel(t)
	m, cmd := typeViewerFilter(t, m, ".a")
	m = deliverViewerFilterResults(t, m, cmd)
	assert.Equal(t, "body is not JSON", m.viewerFilter.err)
	assert.Contains(t, m.viewerContent, "hello world", "the unfiltered body stays visible")
}

func TestViewerFilterDropsStaleResults(t *testing.T) {
	m := jsonViewerTestModel(t, filterTestBody)
	m, cmd := typeViewerFilter(t, m, ".total")
	m = deliverViewerFilterResults(t, m, cmd)
	require.Equal(t, "2", m.viewerCopy)

	stale := viewerFilterResultMsg{seq: m.viewerFilter.seq - 1, output: "stale"}
	m, _ = sendMsg(t, m, stale)
	assert.Equal(t, "2", m.viewerCopy)
}

func TestViewerFilterEscClearsAndRestoresBody(t *testing.T) {
	m := jsonViewerTestModel(t, filterTestBody)
	original := m.viewerContent
	m, cmd := typeViewerFilter(t, m, ".total")
	m = deliverViewerFilterResults(t, m, cmd)
	require.True(t, m.viewerFilter.active)

	m, _ = sendKey(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	assert.False(t, m.viewerFilter.open)
	assert.False(t, m.viewerFilter.active)
	assert.Equal(t, viewerMode, m.mode, "esc in the filter clears it and stays in the viewer")
	assert.Equal(t, original, m.viewerContent)
	assert.Equal(t, filterTestBody, m.viewerCopy)
}

func TestViewerFilterEnterKeepsResultAndReturnsToViewer(t *testing.T) {
	m := jsonViewerTestModel(t, filterTestBody)
	m, cmd := typeViewerFilter(t, m, ".total")
	m = deliverViewerFilterResults(t, m, cmd)

	m, _ = sendKey(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	assert.False(t, m.viewerFilter.open)
	assert.True(t, m.viewerFilter.active)
	assert.Equal(t, "2", m.viewerCopy)
	assert.Contains(t, stripANSI(m.viewViewer()), "filter: .total")

	m, _ = sendKey(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	assert.Equal(t, normalMode, m.mode)
	assert.False(t, m.viewerFilter.active, "closing the viewer drops the filter")
}

func TestViewerFilterEmptyExpressionRestoresBody(t *testing.T) {
	m := jsonViewerTestModel(t, filterTestBody)
	m, cmd := typeViewerFilter(t, m, ".total")
	m = deliverViewerFilterResults(t, m, cmd)

	for range len(".total") {
		m, _ = sendKey(t, m, tea.KeyMsg{Type: tea.KeyBackspace})
	}
	assert.Empty(t, m.viewerFilter.input.Value())
	assert.False(t, m.viewerFilter.pending, "an empty expression starts no evaluation")
	assert.False(t, m.viewerFilter.active)
	assert.Equal(t, filterTestBody, m.viewerCopy)
}

func TestViewerFilterNoResultsMessage(t *testing.T) {
	m := jsonViewerTestModel(t, filterTestBody)
	m, cmd := typeViewerFilter(t, m, ".items[] | select(.id > 10)")
	m = deliverViewerFilterResults(t, m, cmd)
	assert.Empty(t, m.viewerCopy)
	assert.Contains(t, stripANSI(m.viewerContent), "(no results)")
}

func TestEvalViewerFilter(t *testing.T) {
	source := &viewerFilterSource{raw: filterTestBody}
	ctx := context.Background()

	t.Run("identity pretty-prints the body", func(t *testing.T) {
		out, err := evalViewerFilter(ctx, source, ".total")
		require.NoError(t, err)
		assert.Equal(t, "2", out)

		out, err = evalViewerFilter(ctx, source, ".items[0]")
		require.NoError(t, err)
		assert.Equal(t, "{\n  \"id\": 1,\n  \"name\": \"a\"\n}", out)
	})

	t.Run("several results are separated by newlines", func(t *testing.T) {
		out, err := evalViewerFilter(ctx, source, ".items[].name")
		require.NoError(t, err)
		assert.Equal(t, "\"a\"\n\"b\"", out)
	})

	t.Run("syntax error is returned", func(t *testing.T) {
		_, err := evalViewerFilter(ctx, source, ".items[")
		require.Error(t, err)
	})

	t.Run("runtime error is returned", func(t *testing.T) {
		_, err := evalViewerFilter(ctx, source, ".total.x")
		require.Error(t, err)
	})

	t.Run("output above the cap is refused", func(t *testing.T) {
		_, err := evalViewerFilter(ctx, source, `"x" * 9000000`)
		require.ErrorIs(t, err, errViewerFilterTooLarge)
	})

	t.Run("more than one JSON value in the body is an error", func(t *testing.T) {
		two := &viewerFilterSource{raw: "1 2"}
		_, err := evalViewerFilter(ctx, two, ".")
		require.EqualError(t, err, "body holds more than one JSON value")
	})
}

func TestViewerFilterFooterWhilePending(t *testing.T) {
	m := jsonViewerTestModel(t, filterTestBody)
	m, _ = typeViewerFilter(t, m, ".total")
	assert.True(t, m.viewerFilter.pending)
	assert.Contains(t, stripANSI(m.viewViewer()), "filtering...")
	assert.True(t, strings.Contains(m.viewViewer(), "jq:"))
}
