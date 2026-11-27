package tui

import (
	"fmt"

	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/crazy-vedic/quark/internal/exec"
	"github.com/crazy-vedic/quark/internal/keybindings"
	"github.com/crazy-vedic/quark/internal/tui/textedit"
)

// scriptResolverMode is the keybindings mode of the script editor.
const scriptResolverMode = 12

// Script phases shown by the script editor.
const (
	scriptPhasePre  = 0
	scriptPhasePost = 1
)

// scriptEditor edits a request's pre-request and post-response scripts in one
// textarea. The phase that is not shown is kept in pre or post.
type scriptEditor struct {
	area  textarea.Model
	sel   textedit.Selection
	phase int
	pre   string
	post  string
}

func newScriptArea() textarea.Model {
	area := textarea.New()
	area.Placeholder = "JavaScript: req, res, env, assert(cond, msg), console.log()"
	area.CharLimit = 0
	area.SetHeight(8)
	area.SetWidth(80)
	return area
}

func (m Model) beginScriptEdit() Model {
	if m.activeRequest == nil {
		return m.status("error", "Select a request first")
	}
	m.scripts = scriptEditor{
		area: newScriptArea(),
		pre:  m.activeRequest.PreScript,
		post: m.activeRequest.PostScript,
	}
	m.scripts.sel.Collapse(0)
	m.scripts.area.SetWidth(max(20, m.width-8))
	styleTextarea(&m.scripts.area, m.theme)
	m.scripts.area.SetValue(m.scripts.pre)
	m.scripts.area.Focus()
	m.activeField = scriptField
	return m.clearStatus()
}

// stashScriptPhase saves the shown text into its phase slot.
func (e *scriptEditor) stashScriptPhase() {
	if e.phase == scriptPhasePre {
		e.pre = e.area.Value()
	} else {
		e.post = e.area.Value()
	}
}

// switchScriptPhase shows the other phase.
func (m Model) switchScriptPhase() Model {
	e := &m.scripts
	e.stashScriptPhase()
	e.phase = (e.phase + 1) % 2
	e.sel.Collapse(0)
	if e.phase == scriptPhasePre {
		e.area.SetValue(e.pre)
	} else {
		e.area.SetValue(e.post)
	}
	return m
}

func (m Model) handleScriptFieldKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if action, ok := m.resolver.Resolve(scriptResolverMode, 0, msg); ok {
		switch action {
		case "save":
			return m.saveScripts()
		case keybindings.ActionCancel:
			return m.cancelScriptEdit(), nil
		case "switch_phase":
			return m.switchScriptPhase(), nil
		}
	}
	ed := m.newTextEditor()
	b := textedit.Area(&m.scripts.area)
	if cmd, handled := ed.HandleKey(&m.scripts.sel, b, msg); handled {
		return m, cmd
	}
	if m.textKeys().Copy.Match(msg.String()) {
		return m.ctrlCNoSelection(), nil
	}
	var cmd tea.Cmd
	m.scripts.area, cmd = m.scripts.area.Update(msg)
	ed.Sync(&m.scripts.sel, b)
	return m, cmd
}

func (m Model) cancelScriptEdit() Model {
	m.scripts.area.Blur()
	m.activeField = noneField
	m.scripts = scriptEditor{}
	return m
}

// saveScripts stores both scripts on the active request and persists it.
func (m Model) saveScripts() (tea.Model, tea.Cmd) {
	if m.activeRequest == nil {
		return m, nil
	}
	m.scripts.stashScriptPhase()
	m.activeRequest.PreScript = m.scripts.pre
	m.activeRequest.PostScript = m.scripts.post
	m = m.cancelScriptEdit()
	m = m.status("success", "Scripts updated")
	return m, saveRequestCmd(m.ctx, m.writer, m.reader, m.activeRequest)
}

// viewScriptEditor renders the script textarea with a heading for its phase.
func (m Model) viewScriptEditor(maxRows, width int) string {
	title := "Pre-request script"
	if m.scripts.phase == scriptPhasePost {
		title = "Post-response script"
	}
	header := m.styles.titleStyle.Render(title) + "  " +
		m.styles.mutedStyle.Render(
			"("+m.renderHintKeys([]string{"script_switch"}, false)+" switch, "+
				m.renderHintKeys([]string{"script_save"}, false)+" save, "+
				m.renderHintKeys([]string{"script_cancel"}, false)+" cancel)",
		)
	area := m.scripts.area
	area.SetWidth(max(20, width))
	area.SetHeight(max(3, maxRows-2))
	return header + "\n" + area.View()
}

// responseStatus sets the status line for one response: extraction outcomes,
// failed script checks, and the last console line of the scripts.
func (m Model) responseStatus(res *exec.ExecuteResult, logs []string) Model {
	kind, text := extractionStatus(res.Extracted)
	if ck, ct := scriptCheckStatus(res.ScriptChecks); ct != "" {
		if text != "" {
			text += "; "
		}
		text += ct
		if ck == "error" || kind == "" {
			kind = ck
		}
	}
	if n := len(logs); n > 0 {
		if text != "" {
			text += " | "
		}
		text += "console: " + logs[n-1]
		if kind == "" {
			kind = "success"
		}
	}
	if text != "" {
		m = m.status(kind, text)
	}
	return m
}

// scriptCheckStatus summarizes post-response script checks for the status line.
func scriptCheckStatus(checks []exec.ScriptCheck) (kind, text string) {
	if len(checks) == 0 {
		return "", ""
	}
	for _, c := range checks {
		if !c.Passed {
			return "error", "Script check failed: " + c.Rule + ": " + c.Reason
		}
	}
	return "success", fmt.Sprintf("Script checks passed (%d)", len(checks))
}
