package tui_test

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/crazy-vedic/quark/internal/domain"
	"github.com/crazy-vedic/quark/internal/tui"
)

func optionsTestRequest(opts domain.RequestOptions) *domain.Request {
	return &domain.Request{
		ID:           "options-req",
		CollectionID: "options-col",
		Name:         "Options",
		Method:       "GET",
		URL:          "https://example.com",
		Headers:      "{}",
		Options:      opts,
	}
}

func optKey(r rune) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}} }

func optTypeText(t *testing.T, m tui.Model, text string) tui.Model {
	t.Helper()
	for _, r := range text {
		m = callUpdate(t, m, optKey(r))
	}
	return m
}

func openOptionsEditor(t *testing.T, req *domain.Request) tui.Model {
	t.Helper()
	m := newModel(defaultConfig()).WithActiveRequest(req).WithFocus(tui.RequestPane)
	return callUpdate(t, m, optKey('o'))
}

func TestOptionsEditor_OpensWithStoredValues(t *testing.T) {
	follow := false
	req := optionsTestRequest(domain.RequestOptions{
		TimeoutMS:       2500,
		FollowRedirects: &follow,
		MaxRedirects:    intPtrForTUI(3),
		Proxy:           "http://proxy.internal:3128",
		SkipTLSVerify:   true,
	})

	m := openOptionsEditor(t, req)

	assert.Equal(t, tui.OptionsField, m.ActiveField())
	assert.Equal(t, "2.5s", m.OptionsTimeoutValue())
	assert.Equal(t, "3", m.OptionsMaxRedirectsValue())
	assert.Equal(t, "http://proxy.internal:3128", m.OptionsProxyValue())
	assert.Equal(t, 0, m.OptionsCursor())
}

func TestOptionsEditor_SavingUnchangedValuesKeepsThemExactly(t *testing.T) {
	follow := true
	stored := domain.RequestOptions{
		TimeoutMS:       1500,
		FollowRedirects: &follow,
		MaxRedirects:    intPtrForTUI(4),
		Proxy:           "socks5h://127.0.0.1:1080",
		SkipTLSVerify:   true,
	}
	req := optionsTestRequest(stored)
	m := openOptionsEditor(t, req)

	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyEnter})

	assert.Equal(t, tui.NoneField, m.ActiveField())
	assert.Equal(t, stored, m.ActiveRequest().Options)
	assert.Equal(t, "Options updated", m.StatusSuccess())
}

func TestOptionsEditor_EditingRowsSavesOptions(t *testing.T) {
	req := optionsTestRequest(domain.RequestOptions{})
	m := openOptionsEditor(t, req)

	// Row 1: follow redirects, cycled from "default" to "follow".
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyDown})
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRight})
	// Row 2: max redirects, typed.
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyDown})
	m = callUpdate(t, m, optKey('e'))
	m = optTypeText(t, m, "5")
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	// Row 3: proxy, typed.
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyDown})
	m = callUpdate(t, m, optKey('e'))
	m = optTypeText(t, m, "proxy.internal:3128")
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	// Row 4: skip TLS verification, toggled.
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyDown})
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyDown})
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRight})

	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyEnter})

	got := m.ActiveRequest().Options
	require.NotNil(t, got.FollowRedirects)
	assert.True(t, *got.FollowRedirects)
	require.NotNil(t, got.MaxRedirects)
	assert.Equal(t, 5, *got.MaxRedirects)
	assert.Equal(t, "http://proxy.internal:3128", got.Proxy,
		"a bare host:port is stored with the http scheme")
	assert.True(t, got.SkipTLSVerify)
	assert.Zero(t, got.TimeoutMS, "an untouched timeout stays at the default")
}

func TestOptionsEditor_InvalidRowKeepsEditorOpenAndSavesNothing(t *testing.T) {
	cases := map[string]struct {
		row  int
		text string
	}{
		"timeout that is not a duration": {tui.OptionRowTimeout, "soon"},
		"negative redirect limit":        {tui.OptionRowMaxRedirects, "-1"},
		"proxy with unsupported scheme":  {tui.OptionRowProxy, "ftp://files.example"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			stored := domain.RequestOptions{TimeoutMS: 900}
			req := optionsTestRequest(stored)
			m := openOptionsEditor(t, req)
			for range tc.row {
				m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyDown})
			}
			m = callUpdate(t, m, optKey('e'))
			m = optTypeText(t, m, tc.text)
			m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyEnter})
			m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyEnter})

			assert.Equal(t, tui.OptionsField, m.ActiveField(), "the editor stays open")
			assert.NotEmpty(t, m.OptionsError())
			assert.Equal(t, stored, m.ActiveRequest().Options, "nothing is saved")
		})
	}
}

func TestOptionsEditor_EscCancelsWithoutSaving(t *testing.T) {
	stored := domain.RequestOptions{TimeoutMS: 900}
	req := optionsTestRequest(stored)
	m := openOptionsEditor(t, req)
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyDown})
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRight})

	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyEsc})

	assert.Equal(t, tui.NoneField, m.ActiveField())
	assert.Equal(t, stored, m.ActiveRequest().Options)
}

func TestOptionsEditor_EscWhileEditingCancelsLikeOtherEditors(t *testing.T) {
	stored := domain.RequestOptions{TimeoutMS: 900}
	req := optionsTestRequest(stored)
	m := openOptionsEditor(t, req)
	m = callUpdate(t, m, optKey('e'))
	m = optTypeText(t, m, "30s")

	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyEsc})

	assert.Equal(t, tui.NoneField, m.ActiveField(),
		"Esc cancels the editor, as it does for the header and auth editors")
	assert.Equal(t, stored, m.ActiveRequest().Options, "the typed value is discarded")
}

func TestOptionsEditor_ClearedRowsMeanDefaults(t *testing.T) {
	req := optionsTestRequest(domain.RequestOptions{TimeoutMS: 900, Proxy: "http://p.internal:1"})
	m := openOptionsEditor(t, req)
	// Clear the timeout row by starting from an empty text input.
	m = callUpdate(t, m, optKey('e'))
	for range len("900ms") {
		m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyBackspace})
	}
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	// Clear the proxy row the same way.
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyDown})
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyDown})
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyDown})
	m = callUpdate(t, m, optKey('e'))
	for range len("http://p.internal:1") {
		m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyBackspace})
	}
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyEnter})

	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyEnter})

	assert.True(t, m.ActiveRequest().Options.IsZero(),
		"empty rows must store no override, so the configured defaults apply")
}

func TestOptionsEditor_RequestPreviewShowsSummary(t *testing.T) {
	req := optionsTestRequest(domain.RequestOptions{TimeoutMS: 1500})
	m := newModel(defaultConfig())
	m = callUpdate(t, m, tea.WindowSizeMsg{Width: 160, Height: 50})
	m = m.WithActiveRequest(req).WithFocus(tui.RequestPane)

	assert.Contains(t, m.View(), "Options: timeout 1.5s")
}

func TestOptionsEditor_HidesProxyPasswordUnlessEditing(t *testing.T) {
	const proxy = "http://ops:hunter2@proxy.internal:3128" //nolint:gosec // fake password in a fixture
	req := optionsTestRequest(domain.RequestOptions{Proxy: proxy})
	m := newModel(defaultConfig())
	m = callUpdate(t, m, tea.WindowSizeMsg{Width: 160, Height: 50})
	m = m.WithActiveRequest(req).WithFocus(tui.RequestPane)
	m = callUpdate(t, m, optKey('o'))

	view := m.View()
	assert.Contains(t, view, "xxxxx")
	assert.NotContains(t, view, "hunter2", "the password must not be drawn on screen")
	assert.Equal(t, "http://ops:hunter2@proxy.internal:3128", m.OptionsProxyValue(),
		"the stored value is unchanged")
}

func intPtrForTUI(v int) *int { return &v }
