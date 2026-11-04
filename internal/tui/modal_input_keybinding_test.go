package tui

import (
	"testing"
	"unicode"
	"unicode/utf8"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/crazy-vedic/quark/internal/config"
	"github.com/crazy-vedic/quark/internal/curl"
	"github.com/crazy-vedic/quark/internal/keybindings"

	"github.com/stretchr/testify/require"
)

type modalTextInputCase struct {
	name  string
	setup func(Model) Model
}

type textControl struct {
	name    string
	focused func() bool
	value   func() string
}

func TestModalTextInputsAcceptEveryPrintableKeybinding(t *testing.T) {
	configDir := t.TempDir()
	binds := keybindings.DefaultKeybindings()
	entries := keybindings.ListEntries(binds)

	cases := []modalTextInputCase{
		{
			name: "request search",
			setup: func(m Model) Model {
				m, _ = m.openSearch()
				return m
			},
		},
		{
			name: "help search",
			setup: func(m Model) Model {
				m = m.openHelp()
				m.helpSearch = true
				m.searchInput.Focus()
				return m
			},
		},
		{
			name: "response viewer find",
			setup: func(m Model) Model {
				m = m.openViewerForResponse()
				m, _ = m.openViewerFind()
				return m
			},
		},
		{
			name:  "curl command",
			setup: func(m Model) Model { return m.openCurlImport() },
		},
		{
			name: "curl request name",
			setup: func(m Model) Model {
				m, _ = m.openImport(&curl.ImportResult{})
				return m
			},
		},
		{
			name:  "Postman path",
			setup: func(m Model) Model { return m.openPostmanImportModal() },
		},
		{
			name: "collection prompt",
			setup: func(m Model) Model {
				m, _ = m.enterCollectionPrompt(promptAdd, "collection-id")
				return m
			},
		},
		{
			name: "schedule prompt",
			setup: func(m Model) Model {
				m.mode = scheduleMode
				m.scheduleInput.Focus()
				return m
			},
		},
	}

	for _, field := range []struct {
		name  string
		field int
	}{
		{name: "client certificate host", field: clientCertHostField},
		{name: "client certificate type", field: clientCertTypeField},
		{name: "client certificate file", field: clientCertFileField},
		{name: "client certificate key file", field: clientCertKeyFileField},
		{name: "client certificate CA file", field: clientCertCAFileField},
		{name: "client certificate password", field: clientCertPasswordField},
	} {
		cases = append(cases, modalTextInputCase{
			name: field.name,
			setup: func(m Model) Model {
				m = m.openClientCertModal().beginClientCertEdit(-1)
				m.clientCertField = field.field
				m.focusClientCertField()
				return m
			},
		})
	}

	for _, field := range []struct {
		name string
		key  bool
	}{
		{name: "environment variable name", key: true},
		{name: "environment variable value"},
	} {
		cases = append(cases, modalTextInputCase{
			name: field.name,
			setup: func(m Model) Model {
				m.mode = envMode
				m.envEditor = envEditor{
					active:  true,
					editing: true,
					tabs:    []envTab{{ID: "env-id", Name: "test", CollectionID: "collection-id"}},
					vars:    []envVar{{}},
					editKey: textinput.New(),
					editVal: textinput.New(),
				}
				if field.key {
					m.envEditor.editKey.Focus()
				} else {
					m.envEditor.editVal.Focus()
				}
				return m
			},
		})
	}

	for _, input := range cases {
		for _, entry := range entries {
			key, ok := printableBindingRune(entry.Key)
			if !ok {
				continue
			}

			t.Run(input.name+"/"+entry.Action+"="+entry.Key, func(t *testing.T) {
				m := New(Deps{Config: config.Default(configDir), ConfigDir: configDir})
				m = input.setup(m)
				active := focusedTextControls(&m)
				require.NotEmpty(t, active, "modal should have a focused text control")

				updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{key}})
				m = requireModel(t, updated)
				controls := textControls(&m)
				for _, activeControl := range active {
					control, ok := findTextControl(controls, activeControl.name)
					require.True(t, ok, "input inventory lost %q", activeControl.name)
					require.Equal(
						t,
						string(key),
						control.value(),
						"%s: binding consumed instead of typed",
						control.name,
					)
				}
			})
		}
	}
}

// textControls inventories the model's text-entry widgets; focusedTextControls
// discovers which one(s) receive input in the modal state created by each case.
func textControls(m *Model) []textControl {
	controls := []textControl{
		{name: "url", focused: m.urlInput.Focused, value: m.urlInput.Value},
		{name: "request search", focused: m.searchInput.Focused, value: m.searchInput.Value},
		{name: "viewer find", focused: m.viewerFind.Focused, value: m.viewerFind.Value},
		{name: "curl command", focused: m.importInput.Focused, value: m.importInput.Value},
		{name: "curl request name", focused: m.importName.Focused, value: m.importName.Value},
		{name: "Postman path", focused: m.postmanPath.Focused, value: m.postmanPath.Value},
		{name: "collection prompt", focused: m.promptInput.Focused, value: m.promptInput.Value},
		{name: "schedule prompt", focused: m.scheduleInput.Focused, value: m.scheduleInput.Value},
		{
			name:    "client certificate host",
			focused: m.clientCertHost.Focused,
			value:   m.clientCertHost.Value,
		},
		{
			name:    "client certificate type",
			focused: m.clientCertType.Focused,
			value:   m.clientCertType.Value,
		},
		{
			name:    "client certificate file",
			focused: m.clientCertFile.Focused,
			value:   m.clientCertFile.Value,
		},
		{
			name:    "client certificate key file",
			focused: m.clientCertKeyFile.Focused,
			value:   m.clientCertKeyFile.Value,
		},
		{
			name:    "client certificate CA file",
			focused: m.clientCertCAFile.Focused,
			value:   m.clientCertCAFile.Value,
		},
		{
			name:    "client certificate password",
			focused: m.clientCertPassword.Focused,
			value:   m.clientCertPassword.Value,
		},
		{name: "header key", focused: m.headerKeyInput.Focused, value: m.headerKeyInput.Value},
		{name: "header value", focused: m.headerValueInput.Focused, value: m.headerValueInput.Value},
		{name: "auth token", focused: m.authEditor.tokenInput.Focused, value: m.authEditor.tokenInput.Value},
		{name: "auth username", focused: m.authEditor.usernameInput.Focused, value: m.authEditor.usernameInput.Value},
		{name: "auth password", focused: m.authEditor.passwordInput.Focused, value: m.authEditor.passwordInput.Value},
		{name: "auth API key name", focused: m.authEditor.apiKeyNameInput.Focused, value: m.authEditor.apiKeyNameInput.Value},
		{name: "auth API key value", focused: m.authEditor.apiKeyValueInput.Focused, value: m.authEditor.apiKeyValueInput.Value},
		{name: "environment variable name", focused: m.envEditor.editKey.Focused, value: m.envEditor.editKey.Value},
		{name: "environment variable value", focused: m.envEditor.editVal.Focused, value: m.envEditor.editVal.Value},
		{name: "body editor", focused: m.bodyTextarea.Focused, value: m.bodyTextarea.Value},
	}
	return controls
}

func focusedTextControls(m *Model) []textControl {
	var focused []textControl
	for _, control := range textControls(m) {
		if control.focused() {
			focused = append(focused, control)
		}
	}
	return focused
}

func findTextControl(controls []textControl, name string) (textControl, bool) {
	for _, control := range controls {
		if control.name == name {
			return control, true
		}
	}
	return textControl{}, false
}

func printableBindingRune(key string) (rune, bool) {
	r, size := utf8.DecodeRuneInString(key)
	return r, size == len(key) && size > 0 && unicode.IsPrint(r)
}
