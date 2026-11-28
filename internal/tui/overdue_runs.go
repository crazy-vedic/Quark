package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/crazy-vedic/quark/internal/domain"
	"github.com/crazy-vedic/quark/internal/keybindings"
	"github.com/crazy-vedic/quark/internal/store"
)

// overdueRun is a scheduled run that was already due when the TUI started.
type overdueRun struct {
	run   *domain.ScheduledRun
	label string // "Collection / Request"
}

// overdueScheduledRunsMsg asks the user what to do with runs that were due
// before the TUI started.
type overdueScheduledRunsMsg struct {
	runs []overdueRun
}

// overdueRunsCancelledMsg reports the result of skipping overdue runs.
type overdueRunsCancelledMsg struct {
	cancelled int
	err       error
}

// Choices in the overdue runs prompt, in display order.
const (
	overdueChoiceRunAll = iota
	overdueChoiceSkip
	overdueChoiceLater
	overdueChoiceCount
)

var overdueChoiceLabels = [overdueChoiceCount]string{
	"Run all now",
	"Skip: cancel these runs",
	"Decide later: keep them pending, do not run this session",
}

// startupScheduleCmd runs once at startup. Runs that are already due are
// returned for the user to decide on; otherwise it waits for the next run
// like scheduleNextWakeCmd.
func startupScheduleCmd(
	ctx context.Context,
	scheduler store.ScheduledRunStore,
	lister store.CollectionLister,
	reader store.RequestReader,
	now func() time.Time,
	seq int,
) tea.Cmd {
	return func() tea.Msg {
		if scheduler == nil {
			return nil
		}
		runs, err := scheduler.ListDueScheduledRuns(ctx, now())
		if err != nil {
			return errLoadMsg{err: fmt.Errorf("list overdue scheduled runs: %w", err)}
		}
		if len(runs) == 0 {
			return scheduleNextWakeCmd(ctx, scheduler, reader, now, seq, nil)()
		}
		msg := overdueScheduledRunsMsg{runs: make([]overdueRun, 0, len(runs))}
		for _, run := range runs {
			msg.runs = append(msg.runs, overdueRun{
				run:   run,
				label: overdueRunLabel(ctx, lister, reader, run),
			})
		}
		return msg
	}
}

func overdueRunLabel(
	ctx context.Context,
	lister store.CollectionLister,
	reader store.RequestReader,
	run *domain.ScheduledRun,
) string {
	if reader != nil {
		if req, err := reader.GetRequest(ctx, run.RequestID); err == nil && req != nil {
			return scheduledRunSuccessLabel(ctx, lister, req)
		}
	}
	return scheduledRunName(ctx, reader, run)
}

// nextWakeRun returns the earliest pending run that is not in skip.
func nextWakeRun(
	ctx context.Context,
	scheduler store.ScheduledRunReader,
	skip map[string]bool,
) (*domain.ScheduledRun, error) {
	run, err := scheduler.NextPendingScheduledRun(ctx)
	if err != nil || run == nil || !skip[run.ID] {
		return run, err
	}
	runs, err := scheduler.ListScheduledRuns(ctx)
	if err != nil {
		return nil, err
	}
	for _, candidate := range runs {
		if candidate.Status == domain.ScheduledRunPending && !skip[candidate.ID] {
			return candidate, nil
		}
	}
	return nil, nil
}

func cloneRunIDSet(in map[string]bool) map[string]bool {
	out := make(map[string]bool, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

// cancelOverdueRunsCmd marks runs cancelled. A run is claimed first so a run
// another process already sent keeps its completed status.
func cancelOverdueRunsCmd(
	ctx context.Context,
	scheduler store.ScheduledRunWriter,
	runs []*domain.ScheduledRun,
) tea.Cmd {
	return func() tea.Msg {
		if scheduler == nil {
			return overdueRunsCancelledMsg{err: fmt.Errorf("scheduler unavailable")}
		}
		var out overdueRunsCancelledMsg
		var errs []error
		for _, src := range runs {
			claimed, err := scheduler.ClaimScheduledRun(ctx, src.ID)
			if err != nil {
				errs = append(errs, err)
				continue
			}
			if !claimed {
				continue
			}
			run := *src
			run.Status = domain.ScheduledRunCancelled
			run.LastError = ""
			if err := scheduler.SaveScheduledRun(ctx, &run); err != nil {
				errs = append(errs, err)
				continue
			}
			out.cancelled++
		}
		out.err = errors.Join(errs...)
		return out
	}
}

func (m Model) openOverdueRuns(runs []overdueRun) Model {
	if len(runs) == 0 {
		return m
	}
	m.overdueRuns = runs
	m.overdueCursor = overdueChoiceRunAll
	m.mode = overdueRunsMode
	return m
}

func (m Model) closeOverdueRuns() Model {
	m.overdueRuns = nil
	m.overdueCursor = 0
	m.mode = normalMode
	return m
}

func (m Model) overdueScheduledRuns() []*domain.ScheduledRun {
	runs := make([]*domain.ScheduledRun, 0, len(m.overdueRuns))
	for _, o := range m.overdueRuns {
		cloned := *o.run
		runs = append(runs, &cloned)
	}
	return runs
}

func (m Model) handleOverdueRunsKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	action, ok := m.resolver.Resolve(1, 0, msg)
	if !ok {
		return m, nil
	}
	switch action {
	case keybindings.ActionNavigateDown:
		if m.overdueCursor < overdueChoiceCount-1 {
			m.overdueCursor++
		}
		return m, nil
	case keybindings.ActionNavigateUp:
		if m.overdueCursor > 0 {
			m.overdueCursor--
		}
		return m, nil
	case "select":
		return m.applyOverdueChoice(m.overdueCursor)
	case keybindings.ActionCancel:
		return m.applyOverdueChoice(overdueChoiceLater)
	}
	return m, nil
}

func (m Model) applyOverdueChoice(choice int) (tea.Model, tea.Cmd) {
	runs := m.overdueScheduledRuns()
	switch choice {
	case overdueChoiceRunAll:
		runner := m.scheduledRunner()
		ctx := m.ctx
		m = m.closeOverdueRuns()
		// The background result re-arms the timer.
		return m, func() tea.Msg {
			if !runner.ready() {
				return runner.unavailable()
			}
			return runner.execute(ctx, runs)
		}
	case overdueChoiceSkip:
		m = m.closeOverdueRuns()
		return m, cancelOverdueRunsCmd(m.ctx, m.scheduler, runs)
	default:
		deferred := cloneRunIDSet(m.deferredRuns)
		for _, run := range runs {
			deferred[run.ID] = true
		}
		m.deferredRuns = deferred
		m = m.closeOverdueRuns()
		m = m.status("success", fmt.Sprintf(
			"%s left pending; they will not run this session",
			pluralRuns(len(runs)),
		))
		return m.armScheduleTimer()
	}
}

func (m Model) applyOverdueRunsCancelled(msg overdueRunsCancelledMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m = m.status("error", fmt.Sprintf("Cancel scheduled runs failed: %v", msg.err))
	} else {
		m = m.status("success", fmt.Sprintf("Cancelled %s", pluralRuns(msg.cancelled)))
	}
	return m.armScheduleTimer()
}

func pluralRuns(n int) string {
	if n == 1 {
		return "1 overdue scheduled run"
	}
	return fmt.Sprintf("%d overdue scheduled runs", n)
}

// overdueListLimit caps how many runs the prompt lists before "and N more".
const overdueListLimit = 8

func (m Model) viewOverdueRunsModal() string {
	var sb strings.Builder
	sb.WriteString(m.styles.titleStyle.Render("Overdue scheduled runs"))
	sb.WriteString("\n\n")
	fmt.Fprintf(&sb,
		"%s became due while Quark was closed:\n\n",
		pluralRuns(len(m.overdueRuns)),
	)
	width := m.twoThirdsModalWidth() - 6
	for i, o := range m.overdueRuns {
		if i == overdueListLimit {
			sb.WriteString(m.styles.mutedStyle.Render(
				fmt.Sprintf("  … and %d more", len(m.overdueRuns)-overdueListLimit),
			))
			sb.WriteString("\n")
			break
		}
		due := o.run.RunAt.Local().Format("2006-01-02 15:04")
		line := truncate(fmt.Sprintf("  %s  %s", due, o.label), width)
		sb.WriteString(line)
		sb.WriteString("\n")
	}
	sb.WriteString("\n")
	for i, label := range overdueChoiceLabels {
		line := truncate("  "+label, width)
		if i == m.overdueCursor {
			line = m.styles.titleStyle.Render(truncate("> "+label, width))
		}
		sb.WriteString(line)
		sb.WriteString("\n")
	}
	sb.WriteString("\n")
	sb.WriteString(m.styles.mutedStyle.Render(m.renderHints([]hintItem{
		{Label: "choose", Actions: []string{"search_select"}},
		{Label: "navigate", Actions: []string{"search_up", "search_down"}},
		{Label: "decide later", Actions: []string{"search_cancel"}},
	})))

	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(m.styles.accent).
		Padding(1, 2).
		Width(m.twoThirdsModalWidth()).
		Render(sb.String())

	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, box)
}
