package tui_test

import (
	"errors"
	"fmt"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"

	"github.com/crazy-vedic/quark/internal/exec"
	"github.com/crazy-vedic/quark/internal/tui"
)

func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m,
		goleak.IgnoreAnyFunction("github.com/dlclark/regexp2/v2.runClock"),
	)
}

// newTestModel creates a bare model with no dependencies (suitable for Update tests).
func newTestModel() tui.Model {
	return tui.New(tui.Deps{})
}

// callUpdate calls m.Update(msg) and returns the updated Model.
func callUpdate(t *testing.T, m tui.Model, msg tea.Msg) tui.Model {
	t.Helper()
	updated, _ := m.Update(msg)
	model, ok := updated.(tui.Model)
	require.True(t, ok, "Update must return tui.Model")
	return model
}

// --- Tests ---

func TestModel_Update_HttpResponse_ClearsLoadingState(t *testing.T) {
	m := newTestModel()
	m = m.WithLoading(true)

	result := &exec.ExecuteResult{
		StatusCode: 200,
		Status:     "200 OK",
		Body:       []byte(`{"ok":true}`),
		Duration:   100 * time.Millisecond,
	}
	updated := callUpdate(t, m, tui.HttpResponseMsg(result))

	assert.False(t, updated.Loading(), "loading must be cleared after response")
	assert.Nil(t, updated.Err(), "err must be nil on success")
	assert.Equal(t, 200, updated.Response().StatusCode)
}

func TestModel_Update_ErrTimeout_SetsStatusErr(t *testing.T) {
	m := newTestModel()
	m = m.WithLoading(true)

	updated := callUpdate(t, m, tui.HttpErrMsg(
		fmt.Errorf("wrapped: %w", exec.ErrTimeout),
	))

	assert.False(t, updated.Loading())
	assert.NotEmpty(t, updated.StatusErr(), "timeout must set status bar error")
	assert.Nil(t, updated.Err(), "timeout is Tier 2, not fatal")
}

func TestModel_Update_ErrInvalidURL_SetsValidationErr(t *testing.T) {
	m := newTestModel()
	m = m.WithLoading(true)

	updated := callUpdate(t, m, tui.HttpErrMsg(
		fmt.Errorf("bad url: %w", exec.ErrInvalidURL),
	))

	assert.False(t, updated.Loading())
	assert.NotEmpty(t, updated.ValidationErr(), "invalid URL must set validation error")
	assert.Empty(t, updated.StatusErr())
}

func TestModel_Update_ErrCancelled_NoErrorShown(t *testing.T) {
	m := newTestModel()
	m = m.WithLoading(true)

	updated := callUpdate(t, m, tui.HttpErrMsg(
		fmt.Errorf("ctx: %w", exec.ErrRequestCancelled),
	))

	assert.False(t, updated.Loading())
	assert.Empty(t, updated.StatusErr(), "cancel must not show an error")
	assert.Nil(t, updated.Err())
}

func TestModel_Update_EscKey_CancelsInFlightRequest(t *testing.T) {
	m := newTestModel()
	m = m.WithLoading(true)

	cancelled := false
	m = m.WithCancel(func() { cancelled = true })

	callUpdate(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	assert.True(t, cancelled, "Esc must call the cancel func")
}

func TestModel_Update_EscKey_NoOp_WhenNotLoading(t *testing.T) {
	m := newTestModel()
	// Not loading, no cancel func.
	updated := callUpdate(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	assert.False(t, updated.Loading())
}

func TestModel_Update_QuitKey(t *testing.T) {
	m := newTestModel()
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	require.NotNil(t, cmd, "ctrl+c must return a Quit command")
}

func TestModel_Update_UnexpectedError_SetsFatalErr(t *testing.T) {
	m := newTestModel()
	m = m.WithLoading(true)

	sentinel := errors.New("disk full")
	updated := callUpdate(t, m, tui.HttpErrMsg(sentinel))

	assert.False(t, updated.Loading())
	assert.NotNil(t, updated.Err(), "unexpected error must be stored")
	assert.ErrorIs(t, updated.Err(), sentinel)
}

func TestModel_Update_WindowSize(t *testing.T) {
	m := newTestModel()
	updated := callUpdate(t, m, tea.WindowSizeMsg{Width: 120, Height: 40})
	assert.Equal(t, 120, updated.Width())
	assert.Equal(t, 40, updated.Height())
}
