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
	value func(Model) string
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
			value: func(m Model) string { return m.searchInput.Value() },
		},
		{
			name: "help search",
			setup: func(m Model) Model {
				m = m.openHelp()
				m.helpSearch = true
				m.searchInput.Focus()
				return m
			},
			value: func(m Model) string { return m.searchInput.Value() },
		},
		{
			name: "response viewer find",
			setup: func(m Model) Model {
				m = m.openViewerForResponse()
				m, _ = m.openViewerFind()
				return m
			},
			value: func(m Model) string { return m.viewerFind.Value() },
		},
		{
			name:  "curl command",
			setup: func(m Model) Model { return m.openCurlImport() },
			value: func(m Model) string { return m.importInput.Value() },
		},
		{
			name: "curl request name",
			setup: func(m Model) Model {
				m = m.openCurlImport()
				m.importPreview = &curl.ImportResult{}
				m.importName.Focus()
				return m
			},
			value: func(m Model) string { return m.importName.Value() },
		},
		{
			name:  "Postman path",
			setup: func(m Model) Model { return m.openPostmanImportModal() },
			value: func(m Model) string { return m.postmanPath.Value() },
		},
		{
			name: "collection prompt",
			setup: func(m Model) Model {
				m, _ = m.enterCollectionPrompt(promptAdd, "collection-id")
				return m
			},
			value: func(m Model) string { return m.promptInput.Value() },
		},
		{
			name: "schedule prompt",
			setup: func(m Model) Model {
				m.mode = scheduleMode
				m.scheduleInput.Focus()
				return m
			},
			value: func(m Model) string { return m.scheduleInput.Value() },
		},
	}

	for _, field := range []struct {
		name  string
		field int
		value func(Model) string
	}{
		{name: "client certificate host", field: clientCertHostField, value: func(m Model) string { return m.clientCertHost.Value() }},
		{name: "client certificate type", field: clientCertTypeField, value: func(m Model) string { return m.clientCertType.Value() }},
		{name: "client certificate file", field: clientCertFileField, value: func(m Model) string { return m.clientCertFile.Value() }},
		{name: "client certificate key file", field: clientCertKeyFileField, value: func(m Model) string { return m.clientCertKeyFile.Value() }},
		{name: "client certificate CA file", field: clientCertCAFileField, value: func(m Model) string { return m.clientCertCAFile.Value() }},
		{name: "client certificate password", field: clientCertPasswordField, value: func(m Model) string { return m.clientCertPassword.Value() }},
	} {
		field := field
		cases = append(cases, modalTextInputCase{
			name: field.name,
			setup: func(m Model) Model {
				m = m.openClientCertModal().beginClientCertEdit(-1)
				m.clientCertField = field.field
				m.focusClientCertField()
				return m
			},
			value: field.value,
		})
	}

	for _, field := range []struct {
		name  string
		key   bool
		value func(Model) string
	}{
		{name: "environment variable name", key: true, value: func(m Model) string { return m.envEditor.editKey.Value() }},
		{name: "environment variable value", value: func(m Model) string { return m.envEditor.editVal.Value() }},
	} {
		field := field
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
			value: field.value,
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
				updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{key}})
				m = requireModel(t, updated)
				require.Equal(t, string(key), input.value(m), "binding consumed instead of typed")
			})
		}
	}
}

func printableBindingRune(key string) (rune, bool) {
	r, size := utf8.DecodeRuneInString(key)
	return r, size == len(key) && size > 0 && unicode.IsPrint(r)
}
