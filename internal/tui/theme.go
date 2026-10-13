package tui

import (
	"strings"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/lipgloss"

	"github.com/crazy-vedic/quark/internal/theme"
)

// styleSet is derived from a resolved semantic theme. It belongs to a Model,
// never to the package, so two models and a live preview cannot affect each
// other's rendering.
type styleSet struct {
	accent, error, success, warning, info, mutedColor, foreground, match lipgloss.Color
	get, post, put, delete                                               lipgloss.Color
	activeBorder, inactiveBorder                                         lipgloss.Style
	titleStyle, methodStyle, sendButtonStyle, mutedStyle                 lipgloss.Style
	errorStyle, goodStyle, warnStyle, statusStyle                        lipgloss.Style
}

func stylesFor(t theme.Theme) styleSet {
	p := t.Palette
	s := styleSet{
		accent: lipgloss.Color(p.Accent), error: lipgloss.Color(p.Error),
		success: lipgloss.Color(p.Success), warning: lipgloss.Color(p.Warning),
		info: lipgloss.Color(p.Info), mutedColor: lipgloss.Color(p.Muted),
		foreground: lipgloss.Color(p.Foreground), match: lipgloss.Color(p.Match),
		get: lipgloss.Color(p.GET), post: lipgloss.Color(p.POST),
		put: lipgloss.Color(p.PUT), delete: lipgloss.Color(p.DELETE),
	}
	s.activeBorder = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color(p.ActiveBorder))
	s.inactiveBorder = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(s.mutedColor)
	s.titleStyle = lipgloss.NewStyle().Bold(true).Foreground(s.accent)
	s.methodStyle = lipgloss.NewStyle().Bold(true).Foreground(s.info)
	s.sendButtonStyle = lipgloss.NewStyle().Bold(true).Foreground(s.success)
	s.mutedStyle = lipgloss.NewStyle().Foreground(s.mutedColor)
	s.errorStyle = lipgloss.NewStyle().Foreground(s.error)
	s.goodStyle = lipgloss.NewStyle().Foreground(s.success)
	s.warnStyle = lipgloss.NewStyle().Foreground(s.warning)
	s.statusStyle = s.mutedStyle
	return s
}

func (m Model) useTheme(t theme.Theme) Model {
	m.theme = t
	m.styles = stylesFor(t)
	if !theme.IsBuiltin(t.Name) {
		if hash, err := theme.FileHash(m.configDir, t.Name); err == nil {
			m.themeFileHash = hash
			m.themeWatchError = ""
		}
	}
	m.styleInputs()
	// A theme changes the ANSI output even when response data is unchanged.
	// Keep the logical scroll offsets while dropping derived formatting caches.
	m.responseText.InvalidateFormatting()
	m.viewerText.InvalidateFormatting()
	return m
}

func (m *Model) styleInputs() {
	inputs := []*textinput.Model{
		&m.urlInput, &m.searchInput, &m.viewerFind, &m.importName,
		&m.promptInput, &m.scheduleInput, &m.clientCertHost, &m.clientCertFile,
		&m.clientCertType, &m.clientCertKeyFile, &m.clientCertCAFile,
		&m.clientCertPassword, &m.headerKeyInput, &m.headerValueInput,
		&m.authEditor.tokenInput, &m.authEditor.usernameInput,
		&m.authEditor.passwordInput, &m.authEditor.apiKeyNameInput,
		&m.authEditor.apiKeyValueInput, &m.envEditor.editKey, &m.envEditor.editVal,
	}
	for _, input := range inputs {
		styleTextInput(input, m.theme)
	}
	styleTextarea(&m.importInput, m.theme)
	styleTextarea(&m.bodyTextarea, m.theme)
}

func styleTextInput(input *textinput.Model, t theme.Theme) {
	input.PromptStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(t.Info))
	input.TextStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(t.Foreground))
	input.PlaceholderStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(t.Muted))
	input.CompletionStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(t.Muted))
	input.Cursor.Style = lipgloss.NewStyle().Foreground(lipgloss.Color(t.Accent))
}

func styleTextarea(input *textarea.Model, t theme.Theme) {
	for _, style := range []*textarea.Style{&input.FocusedStyle, &input.BlurredStyle} {
		style.Base = lipgloss.NewStyle().Foreground(lipgloss.Color(t.Foreground))
		style.Text = style.Base
		style.Placeholder = lipgloss.NewStyle().Foreground(lipgloss.Color(t.Muted))
		style.Prompt = lipgloss.NewStyle().Foreground(lipgloss.Color(t.Info))
		style.LineNumber = lipgloss.NewStyle().Foreground(lipgloss.Color(t.Muted))
	}
	input.Cursor.Style = lipgloss.NewStyle().Foreground(lipgloss.Color(t.Accent))
	// textarea stores a pointer to its active style. Rebind it after Model
	// copies and theme changes so a focused editor sees the new palette.
	if input.Focused() {
		input.Focus()
	} else {
		input.Blur()
	}
}

// paintThemeFrame fills the terminal when a theme supplies a background.
// Nested Lip Gloss and Chroma styles emit SGR resets; restore the frame colors
// after those resets so unstyled text and gaps remain readable.
func (m Model) paintThemeFrame(content string) string {
	base := lipgloss.NewStyle().
		Foreground(m.styles.foreground).
		Background(lipgloss.Color(m.theme.Background))
	frame := base.Width(m.width).Height(m.height).Render(content)
	prefix := sgrPrefix(base)
	if prefix == "" { // terminals without color support
		return frame
	}
	background := sgrPrefix(lipgloss.NewStyle().Background(lipgloss.Color(m.theme.Background)))
	foreground := sgrPrefix(lipgloss.NewStyle().Foreground(m.styles.foreground))
	frame = strings.ReplaceAll(frame, "\x1b[0m", "\x1b[0m"+prefix)
	frame = strings.ReplaceAll(frame, "\x1b[m", "\x1b[m"+prefix)
	frame = strings.ReplaceAll(frame, "\x1b[49m", "\x1b[49m"+background)
	frame = strings.ReplaceAll(frame, "\x1b[39m", "\x1b[39m"+foreground)
	return frame + "\x1b[0m"
}

func sgrPrefix(style lipgloss.Style) string {
	const marker = "Q"
	rendered := style.Render(marker)
	if i := strings.Index(rendered, marker); i >= 0 {
		return rendered[:i]
	}
	return ""
}
