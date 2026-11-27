package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/crazy-vedic/quark/internal/domain"
)

// collectionSettings is the modal that edits a collection's inherited auth and
// default headers. The auth part uses m.authEditor, which is free while the
// modal is open: the modal owns the keys, and closing it resets the editor.
// Headers are key/value rows edited through the request header inputs.
type collectionSettings struct {
	colID   string
	name    string
	headers []headerPair
	cursor  int
	// editing is set while a row is being edited. editHeader is the header
	// index for a header row; editField is 0 for the key and 1 for the value.
	editing    bool
	editHeader int
	editField  int
	// addedHeader marks a header created by the current edit, so cancelling
	// the edit removes it again.
	addedHeader bool
	err         string
}

// collectionSettingsAuthType maps a stored collection auth type to the type
// the auth editor shows. An empty or inherit type shows as Inherit, so a
// collection that sets nothing reads as inheriting its parent.
func collectionSettingsAuthType(stored string) string {
	switch strings.ToLower(strings.TrimSpace(stored)) {
	case "", domain.AuthTypeInherit:
		return domain.AuthTypeInherit
	case "none":
		return domain.AuthTypeNone
	default:
		return domain.NormalizeAuthType(stored)
	}
}

// parseCollectionHeaders decodes a collection's default headers. Values may be
// one string or a list of strings, as request headers may be.
func parseCollectionHeaders(raw string) (http.Header, error) {
	headers := make(http.Header)
	if strings.TrimSpace(raw) == "" {
		return headers, nil
	}
	var entries map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &entries); err != nil {
		return nil, fmt.Errorf("parse collection headers: %w", err)
	}
	for key, encoded := range entries {
		var values []string
		if err := json.Unmarshal(encoded, &values); err != nil {
			var single string
			if err := json.Unmarshal(encoded, &single); err != nil {
				return nil, fmt.Errorf("parse collection header %q: %w", key, err)
			}
			values = []string{single}
		}
		for _, value := range values {
			headers.Add(key, value)
		}
	}
	return headers, nil
}

// openCollectionSettings opens the settings modal for the selected collection.
func (m Model) openCollectionSettings() Model {
	col := m.selectedCollection()
	if col == nil {
		return m.status("error", "Select a collection first")
	}
	state := collectionSettings{colID: col.ID, name: col.Name}
	m.authEditor = newAuthEditor(&domain.Request{
		AuthType:   collectionSettingsAuthType(col.AuthType),
		AuthConfig: col.AuthConfig,
	})
	if headers, err := parseCollectionHeaders(col.Headers); err == nil {
		keys := make([]string, 0, len(headers))
		for key := range headers {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			for _, value := range headers[key] {
				state.headers = append(state.headers, headerPair{Key: key, Value: value})
			}
		}
	}
	m.collSettings = state
	m.mode = collectionSettingsMode
	return m
}

// closeCollectionSettings discards the draft and returns to normal mode.
func (m Model) closeCollectionSettings() Model {
	m.headerKeyInput.Blur()
	m.headerValueInput.Blur()
	m.mode = normalMode
	m.collSettings = collectionSettings{}
	m.authEditor = authEditor{}
	return m
}

// collSettingsRowCount is the number of rows: the auth rows, one row per
// header, then the add-header row.
func (m Model) collSettingsRowCount() int {
	return len(m.authEditor.rows()) + len(m.collSettings.headers) + 1
}

func (m Model) isCollSettingsAuthRow(i int) bool { return i < len(m.authEditor.rows()) }

func (m Model) isCollSettingsHeaderRow(i int) bool {
	authRows := len(m.authEditor.rows())
	return i >= authRows && i < authRows+len(m.collSettings.headers)
}

func (m Model) isCollSettingsAddRow(i int) bool { return i == m.collSettingsRowCount()-1 }

func (m *Model) clampCollSettingsCursor() {
	s := &m.collSettings
	if s.cursor < 0 {
		s.cursor = 0
	}
	if n := m.collSettingsRowCount(); s.cursor >= n {
		s.cursor = n - 1
	}
}

// handleCollectionSettingsKey routes keys in the modal. While a row is being
// edited, keys go to its input; otherwise they move between rows and act on
// them.
func (m Model) handleCollectionSettingsKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.collSettings.editing {
		return m.handleCollectionSettingsEditKey(msg)
	}
	s := &m.collSettings
	switch msg.String() {
	case "esc", "q":
		return m.closeCollectionSettings(), nil
	case "up", "k":
		s.cursor--
		m.clampCollSettingsCursor()
	case "down", "j":
		s.cursor++
		m.clampCollSettingsCursor()
	case "left", "right":
		if s.cursor == 0 {
			delta := 1
			if msg.String() == "left" {
				delta = -1
			}
			m.authEditor.cycleType(delta)
			m.clampCollSettingsCursor()
		}
	case "enter":
		return m.activateCollectionSettingsRow()
	case "a":
		return m.addCollectionSettingsHeader(), textinput.Blink
	case "d":
		if m.isCollSettingsHeaderRow(s.cursor) {
			i := s.cursor - len(m.authEditor.rows())
			s.headers = append(s.headers[:i], s.headers[i+1:]...)
			m.clampCollSettingsCursor()
		}
	case "s", "ctrl+s":
		return m.saveCollectionSettings()
	}
	return m, nil
}

// activateCollectionSettingsRow starts editing the current row. The type row
// cycles the type.
func (m Model) activateCollectionSettingsRow() (tea.Model, tea.Cmd) {
	s := &m.collSettings
	switch {
	case s.cursor == 0:
		m.authEditor.cycleType(1)
		m.clampCollSettingsCursor()
	case m.isCollSettingsAuthRow(s.cursor):
		m.authEditor.cursor = s.cursor
		m.authEditor.beginEdit()
		if m.authEditor.editing {
			s.editing = true
			return m, textinput.Blink
		}
	case m.isCollSettingsHeaderRow(s.cursor):
		m.beginCollectionHeaderEdit(s.cursor-len(m.authEditor.rows()), false)
		return m, textinput.Blink
	case m.isCollSettingsAddRow(s.cursor):
		return m.addCollectionSettingsHeader(), textinput.Blink
	}
	return m, nil
}

func (m Model) addCollectionSettingsHeader() Model {
	s := &m.collSettings
	s.headers = append(s.headers, headerPair{})
	s.cursor = len(m.authEditor.rows()) + len(s.headers) - 1
	m.beginCollectionHeaderEdit(len(s.headers)-1, true)
	return m
}

func (m *Model) beginCollectionHeaderEdit(index int, added bool) {
	s := &m.collSettings
	s.editing = true
	s.editHeader = index
	s.editField = 0
	s.addedHeader = added
	m.headerKeyInput.SetValue(s.headers[index].Key)
	m.headerValueInput.SetValue(s.headers[index].Value)
	m.headerKeyInput.Focus()
	m.headerValueInput.Blur()
}

// handleCollectionSettingsEditKey handles keys while a row is open for edit.
func (m Model) handleCollectionSettingsEditKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	s := &m.collSettings
	if m.isCollSettingsAuthRow(s.cursor) {
		switch msg.String() {
		case "esc", "enter", "tab":
			m.authEditor.endEdit()
			s.editing = false
			return m, nil
		}
		_, input := m.authEditor.activeInput(m.authEditor.currentRow())
		if input == nil {
			return m, nil
		}
		var cmd tea.Cmd
		*input, cmd = input.Update(msg)
		return m, cmd
	}

	switch msg.String() {
	case "esc":
		if s.addedHeader && s.headers[s.editHeader].Key == "" {
			s.headers = append(s.headers[:s.editHeader], s.headers[s.editHeader+1:]...)
		}
		m.endCollectionHeaderEdit()
		m.clampCollSettingsCursor()
		return m, nil
	case "tab":
		s.editField = 1 - s.editField
		m.focusCollectionHeaderField()
		return m, nil
	case "enter":
		if s.editField == 0 {
			s.editField = 1
			m.focusCollectionHeaderField()
			return m, nil
		}
		s.headers[s.editHeader] = headerPair{
			Key:   strings.TrimSpace(m.headerKeyInput.Value()),
			Value: m.headerValueInput.Value(),
		}
		m.endCollectionHeaderEdit()
		return m, nil
	}
	var cmd tea.Cmd
	if s.editField == 0 {
		m.headerKeyInput, cmd = m.headerKeyInput.Update(msg)
	} else {
		m.headerValueInput, cmd = m.headerValueInput.Update(msg)
	}
	return m, cmd
}

func (m *Model) focusCollectionHeaderField() {
	if m.collSettings.editField == 0 {
		m.headerKeyInput.Focus()
		m.headerValueInput.Blur()
		return
	}
	m.headerValueInput.Focus()
	m.headerKeyInput.Blur()
}

func (m *Model) endCollectionHeaderEdit() {
	m.collSettings.editing = false
	m.collSettings.addedHeader = false
	m.headerKeyInput.Blur()
	m.headerValueInput.Blur()
	m.headerKeyInput.SetValue("")
	m.headerValueInput.SetValue("")
}

// saveCollectionSettings writes the draft auth and headers to the store and
// updates the collection in memory.
func (m Model) saveCollectionSettings() (tea.Model, tea.Cmd) {
	s := m.collSettings
	if s.editing {
		return m.status("error", "Finish editing the row first"), nil
	}
	if m.collDefaults == nil {
		return m.status("error", "Collection settings are not available"), nil
	}
	defaults, err := collectionDefaultsFromDraft(m.authEditor, s.headers)
	if err != nil {
		m.collSettings.err = err.Error()
		return m, nil
	}
	if err := m.collDefaults.SetCollectionDefaults(
		m.contextOrBackground(), s.colID, defaults,
	); err != nil {
		m.collSettings.err = err.Error()
		return m, nil
	}
	for _, c := range m.collections {
		if c.ID == s.colID {
			c.AuthType = defaults.AuthType
			c.AuthConfig = defaults.AuthConfig
			c.Headers = defaults.Headers
		}
	}
	m = m.closeCollectionSettings()
	return m.status("success", fmt.Sprintf("Saved defaults for %q", s.name)), nil
}

// collectionDefaultsFromDraft turns the draft into the stored form. None is
// stored as "none", because an empty collection auth type means inherit.
func collectionDefaultsFromDraft(ed authEditor, headers []headerPair) (*domain.Collection, error) {
	c := &domain.Collection{AuthType: ed.authType, AuthConfig: "{}"}
	switch ed.authType {
	case domain.AuthTypeNone:
		c.AuthType = "none"
	case domain.AuthTypeInherit:
		c.AuthType = domain.AuthTypeInherit
	default:
		c.AuthConfig = marshalAuthConfig(ed.config())
	}

	merged := make(http.Header)
	for _, h := range headers {
		key := strings.TrimSpace(h.Key)
		if key == "" {
			continue
		}
		merged.Add(key, h.Value)
	}
	encoded, err := json.Marshal(map[string][]string(merged))
	if err != nil {
		return nil, fmt.Errorf("encode headers: %w", err)
	}
	c.Headers = string(encoded)
	return c, nil
}

// contextOrBackground returns the TUI root context, or Background when unset.
func (m Model) contextOrBackground() context.Context {
	if m.ctx != nil {
		return m.ctx
	}
	return context.Background()
}

// viewCollectionSettingsModal renders the modal.
func (m Model) viewCollectionSettingsModal() string {
	s := m.collSettings
	var sb strings.Builder
	sb.WriteString(m.styles.titleStyle.Render("Collection settings: "+s.name) + "\n")
	sb.WriteString(m.styles.mutedStyle.Render(
		"Requests in this collection and its children inherit these unless they set their own.",
	) + "\n\n")

	line := func(i int, text string) {
		if i == s.cursor {
			sb.WriteString("▸ " + lipgloss.NewStyle().Bold(true).Render(text) + "\n")
			return
		}
		sb.WriteString("  " + text + "\n")
	}

	muted := m.styles.mutedStyle
	for i, id := range m.authEditor.rows() {
		label := authRowLabel(id)
		if id == authRowType {
			label = "Auth"
		}
		value := m.authEditor.valueForRow(id, s.editing && i == s.cursor, muted)
		line(i, fmt.Sprintf("%-14s %s", label+":", value))
	}
	base := len(m.authEditor.rows())
	for i, h := range s.headers {
		if s.editing && base+i == s.cursor {
			line(base+i, fmt.Sprintf("Header  %s : %s",
				m.headerKeyInput.View(), m.headerValueInput.View()))
			continue
		}
		key := h.Key
		if key == "" {
			key = "(new)"
		}
		value := h.Value
		if value == "" {
			value = "(empty)"
		}
		line(base+i, fmt.Sprintf("Header  %s: %s", key, value))
	}
	line(m.collSettingsRowCount()-1, "+ Add header")

	if s.err != "" {
		sb.WriteString("\n" + m.styles.mutedStyle.Render("Error: "+s.err) + "\n")
	}
	sb.WriteString("\n" + m.styles.mutedStyle.Render(
		"enter edit · ←/→ type · a add header · d delete header · s save · esc cancel"))
	return sb.String()
}
