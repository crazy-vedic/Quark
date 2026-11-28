package tui

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/itchyny/gojq"

	"github.com/crazy-vedic/quark/internal/highlight"
	"github.com/crazy-vedic/quark/internal/tui/textedit"
)

// Response viewer filter. A jq expression (gojq, a pure-Go jq
// implementation) is applied to the JSON body shown in the viewer. Each
// edit starts an evaluation in a tea.Cmd, so decoding and running the
// expression on a large body never blocks Update; results carry a sequence
// number and stale ones are dropped.

const (
	viewerFilterTimeout   = 5 * time.Second
	viewerFilterMaxOutput = 8 << 20
)

var errViewerFilterTooLarge = fmt.Errorf(
	"result exceeds %d MiB", viewerFilterMaxOutput>>20,
)

// viewerFilterState is the filter input and the state of the last
// evaluation for the open viewer.
type viewerFilterState struct {
	input  textinput.Model
	open   bool   // input has focus
	active bool   // a successful result replaces the viewer content
	err    string // last evaluation error, shown in the footer
	seq    int    // sequence of the latest evaluation; older results are dropped
	// pending is true while the latest evaluation has not reported back.
	pending bool
	cancel  context.CancelFunc
	// source is the raw text the viewer was opened with, decoded once and
	// shared read-only by evaluations.
	source *viewerFilterSource
	// baseContent and baseCopy restore the viewer when the filter is cleared.
	baseContent string
	baseCopy    string
}

// viewerFilterSource decodes the viewer body at most once, off the Update loop.
type viewerFilterSource struct {
	raw   string
	once  sync.Once
	value any
	err   error
}

func (s *viewerFilterSource) decoded() (any, error) {
	s.once.Do(func() {
		dec := json.NewDecoder(strings.NewReader(s.raw))
		if err := dec.Decode(&s.value); err != nil {
			s.err = errors.New("body is not JSON")
			return
		}
		if dec.More() {
			s.err = errors.New("body holds more than one JSON value")
		}
	})
	return s.value, s.err
}

// viewerFilterResultMsg reports one evaluation. rendered is output with JSON
// highlighting applied; it is built in the command so Update only swaps it in.
type viewerFilterResultMsg struct {
	seq      int
	output   string
	rendered string
	err      error
}

// resetViewerFilter clears filter state when the viewer opens or closes.
func (m *Model) resetViewerFilter() {
	if m.viewerFilter.cancel != nil {
		m.viewerFilter.cancel()
	}
	input := m.viewerFilter.input
	input.SetValue("")
	input.Blur()
	m.viewerFilter = viewerFilterState{
		input:       input,
		seq:         m.viewerFilter.seq + 1,
		source:      &viewerFilterSource{raw: m.viewerCopy},
		baseContent: m.viewerContent,
		baseCopy:    m.viewerCopy,
	}
	m.viewerFilterSelection = textedit.Selection{}
}

// openViewerFilter focuses the filter input, keeping any current expression.
func (m Model) openViewerFilter() (Model, tea.Cmd) {
	if m.viewerFilter.source == nil {
		m.resetViewerFilter()
	}
	m.viewerFindOpen = false
	m.viewerFind.Blur()
	m.viewerFilter.open = true
	m.viewerFilter.input.CursorEnd()
	return m, m.viewerFilter.input.Focus()
}

// clearViewerFilter drops the filter and restores the unfiltered body.
func (m Model) clearViewerFilter() Model {
	if m.viewerFilter.cancel != nil {
		m.viewerFilter.cancel()
		m.viewerFilter.cancel = nil
	}
	m.viewerFilter.seq++
	m.viewerFilter.open = false
	m.viewerFilter.active = false
	m.viewerFilter.pending = false
	m.viewerFilter.err = ""
	m.viewerFilter.input.SetValue("")
	m.viewerFilter.input.Blur()
	m.viewerFilterSelection = textedit.Selection{}
	m.setViewerBody(m.viewerFilter.baseContent, m.viewerFilter.baseCopy)
	return m
}

// setViewerBody swaps the viewer text and re-runs any open find query.
func (m *Model) setViewerBody(content, copyText string) {
	m.viewerContent = content
	m.viewerCopy = copyText
	m.viewerText.SetContent(content)
	m.viewerText.offset = 0
	if strings.TrimSpace(m.viewerFind.Value()) != "" {
		m.viewerFindFirstMatch()
	} else {
		m.viewerLastMatch = -1
		m.viewerMatches = nil
	}
}

func (m Model) handleViewerFilterKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	keys := m.textKeys()
	ed := m.newTextEditor()
	b := textedit.Input(&m.viewerFilter.input)
	before := m.viewerFilter.input.Value()
	if cmd, handled := ed.HandleKey(&m.viewerFilterSelection, b, msg); handled {
		return m.afterViewerFilterEdit(before, cmd)
	}
	if keys.Copy.Match(key) {
		return m.ctrlCNoSelection(), nil
	}
	switch {
	case keys.ViewerClose.Match(key):
		return m.clearViewerFilter(), nil
	case msg.Type == tea.KeyEnter:
		// Keep the result and hand keys back to the viewer.
		m.viewerFilter.open = false
		m.viewerFilter.input.Blur()
		return m, nil
	}
	var cmd tea.Cmd
	m.viewerFilter.input, cmd = m.viewerFilter.input.Update(msg)
	ed.Sync(&m.viewerFilterSelection, b)
	return m.afterViewerFilterEdit(before, cmd)
}

func (m Model) afterViewerFilterEdit(before string, cmd tea.Cmd) (tea.Model, tea.Cmd) {
	if m.viewerFilter.input.Value() == before {
		return m, cmd
	}
	m, evalCmd := m.startViewerFilter()
	return m, tea.Batch(cmd, evalCmd)
}

// startViewerFilter cancels any running evaluation and starts one for the
// current expression. An empty expression restores the unfiltered body.
func (m Model) startViewerFilter() (Model, tea.Cmd) {
	if m.viewerFilter.cancel != nil {
		m.viewerFilter.cancel()
		m.viewerFilter.cancel = nil
	}
	m.viewerFilter.seq++
	expr := strings.TrimSpace(m.viewerFilter.input.Value())
	if expr == "" {
		m.viewerFilter.active = false
		m.viewerFilter.pending = false
		m.viewerFilter.err = ""
		m.setViewerBody(m.viewerFilter.baseContent, m.viewerFilter.baseCopy)
		return m, nil
	}
	parent := m.ctx
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithTimeout(parent, viewerFilterTimeout)
	m.viewerFilter.cancel = cancel
	m.viewerFilter.pending = true
	cmd := viewerFilterCmd(
		ctx, cancel, m.viewerFilter.seq, m.viewerFilter.source, expr, m.theme.Syntax,
	)
	return m, cmd
}

func viewerFilterCmd(
	ctx context.Context,
	cancel context.CancelFunc,
	seq int,
	source *viewerFilterSource,
	expr, syntax string,
) tea.Cmd {
	return func() tea.Msg {
		defer cancel()
		output, err := evalViewerFilter(ctx, source, expr)
		msg := viewerFilterResultMsg{seq: seq, output: output, err: err}
		if err == nil && output != "" {
			msg.rendered = highlight.JSON(output, syntax)
		}
		return msg
	}
}

// evalViewerFilter runs expr against the decoded source. Multiple results
// are printed one after another, each pretty-printed, like jq.
func evalViewerFilter(
	ctx context.Context,
	source *viewerFilterSource,
	expr string,
) (string, error) {
	query, err := gojq.Parse(expr)
	if err != nil {
		return "", err
	}
	code, err := gojq.Compile(query)
	if err != nil {
		return "", err
	}
	input, err := source.decoded()
	if err != nil {
		return "", err
	}
	var out bytes.Buffer
	iter := code.RunWithContext(ctx, input)
	for {
		v, ok := iter.Next()
		if !ok {
			break
		}
		if err, isErr := v.(error); isErr {
			if errors.Is(err, context.DeadlineExceeded) {
				return "", fmt.Errorf("filter timed out after %v", viewerFilterTimeout)
			}
			return "", err
		}
		encoded, err := gojq.Marshal(v)
		if err != nil {
			return "", err
		}
		if out.Len() > 0 {
			out.WriteByte('\n')
		}
		if err := json.Indent(&out, encoded, "", "  "); err != nil {
			out.Write(encoded)
		}
		if out.Len() > viewerFilterMaxOutput {
			return "", errViewerFilterTooLarge
		}
	}
	return out.String(), nil
}

// applyViewerFilterResult shows a result if it belongs to the latest edit.
func (m Model) applyViewerFilterResult(msg viewerFilterResultMsg) Model {
	if m.mode != viewerMode || msg.seq != m.viewerFilter.seq {
		return m
	}
	m.viewerFilter.pending = false
	m.viewerFilter.cancel = nil
	if msg.err != nil {
		// Keep the last good result on screen while the expression is
		// being typed; the footer shows why this one failed.
		m.viewerFilter.err = msg.err.Error()
		return m
	}
	m.viewerFilter.err = ""
	m.viewerFilter.active = true
	content := msg.rendered
	if msg.output == "" {
		content = m.styles.mutedStyle.Render("(no results)")
	}
	m.setViewerBody(content, msg.output)
	return m
}

// viewerFilterStatus is the footer text describing the filter.
func (m Model) viewerFilterStatus() string {
	switch {
	case m.viewerFilter.err != "":
		return "  error: " + firstLine(m.viewerFilter.err)
	case m.viewerFilter.pending:
		return "  filtering..."
	case m.viewerFilter.active && !m.viewerFilter.open:
		return "  filter: " + strings.TrimSpace(m.viewerFilter.input.Value())
	}
	return ""
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
