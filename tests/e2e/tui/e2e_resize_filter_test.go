//go:build e2e

package tui_test

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/crazy-vedic/quark/internal/tui"
)

// resizeStubModel is a tea.Model that is not a tui.Model, used to check that
// the coalescer passes window sizes through when it cannot read the size.
type resizeStubModel struct{}

func (resizeStubModel) Init() tea.Cmd                       { return nil }
func (resizeStubModel) Update(tea.Msg) (tea.Model, tea.Cmd) { return resizeStubModel{}, nil }
func (resizeStubModel) View() string                        { return "" }

func TestResizeCoalescer_Filter(t *testing.T) {
	sized := resize(t, newE2EModel(t, setupStore(t), &mockExecutor{}), 120, 40)

	t.Run("non window size message passes through", func(t *testing.T) {
		key := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}}
		assert.Equal(t, key, tui.NewResizeCoalescer().Filter(sized, key))
	})

	t.Run("non tui model passes window size through", func(t *testing.T) {
		ws := tea.WindowSizeMsg{Width: 100, Height: 30}
		assert.Equal(t, ws, tui.NewResizeCoalescer().Filter(resizeStubModel{}, ws))
	})

	t.Run("unsized model passes window size through", func(t *testing.T) {
		unsized := newE2EModel(t, setupStore(t), &mockExecutor{})
		ws := tea.WindowSizeMsg{Width: 120, Height: 40}
		assert.Equal(t, ws, tui.NewResizeCoalescer().Filter(unsized, ws))
	})

	t.Run("identical size is dropped", func(t *testing.T) {
		ws := tea.WindowSizeMsg{Width: 120, Height: 40}
		assert.Nil(t, tui.NewResizeCoalescer().Filter(sized, ws))
	})

	t.Run("height change goes through as a window size", func(t *testing.T) {
		ws := tea.WindowSizeMsg{Width: 120, Height: 30}
		assert.Equal(t, ws, tui.NewResizeCoalescer().Filter(sized, ws))
	})

	t.Run("width growth goes through as a window size", func(t *testing.T) {
		ws := tea.WindowSizeMsg{Width: 140, Height: 40}
		assert.Equal(t, ws, tui.NewResizeCoalescer().Filter(sized, ws))
	})

	t.Run("pure width shrink takes the soft path and applies", func(t *testing.T) {
		ws := tea.WindowSizeMsg{Width: 110, Height: 40}
		soft := tui.NewResizeCoalescer().Filter(sized, ws)
		require.NotNil(t, soft)
		_, isWindowSize := soft.(tea.WindowSizeMsg)
		assert.False(t, isWindowSize, "a pure width shrink must not repaint through WindowSizeMsg")

		// The soft message must be handled by the model (update.go) without
		// panicking and must leave a frame that fits the new width.
		updated := callUpdate(t, sized, soft)
		assertViewContains(t, updated, "Collections")
	})
}
