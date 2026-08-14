// Package timing provides opt-in wall-clock timing for long-running paths.
package timing

import (
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

type ReportFormat string

const (
	ReportFormatTree ReportFormat = "tree"
	ReportFormatFlat ReportFormat = "flat"
	ReportFormatOff  ReportFormat = "off"
)

func ParseReportFormat(value string) (ReportFormat, error) {
	switch ReportFormat(strings.ToLower(strings.TrimSpace(value))) {
	case "", ReportFormatTree:
		return ReportFormatTree, nil
	case ReportFormatFlat:
		return ReportFormatFlat, nil
	case ReportFormatOff:
		return ReportFormatOff, nil
	default:
		return "", fmt.Errorf("invalid timing format %q (want tree, flat, or off)", value)
	}
}

type sample struct {
	count int
	total time.Duration
	max   time.Duration
}

type node struct {
	name     string
	sample   sample
	children map[string]*node
}

// Collector aggregates timing samples by operation name.
type Collector struct {
	enabled bool
	mu      sync.Mutex
	samples map[string]sample
	roots   map[string]*node
}

func New(enabled bool) *Collector {
	return &Collector{
		enabled: enabled,
		samples: make(map[string]sample),
		roots:   make(map[string]*node),
	}
}

// FromEnv enables collection when QUARK_TIMING is 1, true, or yes.
func FromEnv() *Collector {
	value := os.Getenv("QUARK_TIMING")
	enabled, _ := strconv.ParseBool(value)
	if value == "1" || value == "yes" {
		enabled = true
	}
	return New(enabled)
}

var processDefault = FromEnv()

func Default() *Collector { return processDefault }

func (c *Collector) Enabled() bool { return c != nil && c.enabled }

// Span is a single timed operation. Pass a parent span to Track to build a
// timing tree; a nil parent records the operation as a root.
type Span struct {
	collector *Collector
	operation string
	parent    *Span
	started   time.Time
	noop      bool
	done      sync.Once
}

// Track starts an operation and returns a span intended for defer:
//
//	span := timing.Track("parent", nil)
//	defer span.Done()
//	child := timing.Track("child", span)
//	defer child.Done()
func (c *Collector) Track(operation string, parent ...*Span) *Span {
	var p *Span
	if len(parent) > 0 {
		p = parent[0]
	}
	if !c.Enabled() {
		return &Span{noop: true}
	}
	return &Span{collector: c, operation: operation, parent: p, started: time.Now()}
}

func (s *Span) Done() {
	if s == nil || s.noop || s.collector == nil {
		return
	}
	s.done.Do(func() {
		s.collector.recordSpan(s, time.Since(s.started))
	})
}

func (c *Collector) record(operation string, duration time.Duration) {
	c.recordPath([]string{operation}, duration)
}

func (c *Collector) recordSpan(span *Span, duration time.Duration) {
	var path []string
	for current := span; current != nil && !current.noop; current = current.parent {
		path = append(path, current.operation)
	}
	for i, j := 0, len(path)-1; i < j; i, j = i+1, j-1 {
		path[i], path[j] = path[j], path[i]
	}
	c.recordPath(path, duration)
}

func (c *Collector) recordPath(path []string, duration time.Duration) {
	if len(path) == 0 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	operation := path[len(path)-1]
	value := c.samples[operation]
	value.count++
	value.total += duration
	if duration > value.max {
		value.max = duration
	}
	c.samples[operation] = value

	children := c.roots
	var current *node
	for _, name := range path {
		current = children[name]
		if current == nil {
			current = &node{name: name, children: make(map[string]*node)}
			children[name] = current
		}
		children = current.children
	}
	current.sample.count++
	current.sample.total += duration
	if duration > current.sample.max {
		current.sample.max = duration
	}
}

type reportRow struct {
	operation string
	sample
}

const maxReportRows = 30

// Report writes the default tree report.
func (c *Collector) Report(w io.Writer, limit int) {
	c.ReportTree(w, limit)
}

// ReportFlat writes operations ordered by cumulative total duration. Reports
// are capped at the top 30 operations, or fewer when fewer samples exist.
func (c *Collector) ReportFlat(w io.Writer, limit int) {
	if !c.Enabled() || w == nil {
		return
	}
	c.mu.Lock()
	rows := make([]reportRow, 0, len(c.samples))
	for operation, value := range c.samples {
		if value.max <= 0 {
			continue
		}
		rows = append(rows, reportRow{operation: operation, sample: value})
	}
	c.mu.Unlock()
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].total == rows[j].total {
			if rows[i].max == rows[j].max {
				return rows[i].operation < rows[j].operation
			}
			return rows[i].max > rows[j].max
		}
		return rows[i].total > rows[j].total
	})
	if limit <= 0 || limit > maxReportRows {
		limit = maxReportRows
	}
	if limit > len(rows) {
		limit = len(rows)
	}

	fmt.Fprintf(w, "\n=== Quark performance: slowest %d operations ===\n", limit)
	if limit == 0 {
		fmt.Fprintln(w, "No timing samples recorded.")
		return
	}
	fmt.Fprintf(w, "%-36s %7s %12s %12s %12s\n", "Operation", "Calls", "Average", "Max", "Total")
	for _, row := range rows[:limit] {
		average := row.total / time.Duration(row.count)
		fmt.Fprintf(w, "%-36s %7d %12s %12s %12s\n",
			row.operation, row.count, formatDuration(average),
			formatDuration(row.max), formatDuration(row.total))
	}
}

// ReportTree writes root operations and their children ordered by cumulative
// total duration. The root list is capped at the top 30 operations.
func (c *Collector) ReportTree(w io.Writer, limit int) {
	if !c.Enabled() || w == nil {
		return
	}
	c.mu.Lock()
	roots := make([]*node, 0, len(c.roots))
	for _, root := range c.roots {
		if root.sample.max <= 0 {
			continue
		}
		roots = append(roots, cloneNode(root))
	}
	c.mu.Unlock()

	sortNodes(roots)
	if limit <= 0 || limit > maxReportRows {
		limit = maxReportRows
	}
	if limit > len(roots) {
		limit = len(roots)
	}

	fmt.Fprintf(w, "\n=== Quark timing tree: slowest %d root operations ===\n", limit)
	if limit == 0 {
		fmt.Fprintln(w, "No timing samples recorded.")
		return
	}
	fmt.Fprintf(w, "%-44s %7s %12s %12s %12s\n", "Operation", "Calls", "Average", "Max", "Total")
	for _, root := range roots[:limit] {
		writeNode(w, root, "", true)
	}
}

func cloneNode(src *node) *node {
	dst := &node{name: src.name, sample: src.sample, children: make(map[string]*node, len(src.children))}
	for name, child := range src.children {
		dst.children[name] = cloneNode(child)
	}
	return dst
}

func sortedChildren(n *node) []*node {
	children := make([]*node, 0, len(n.children))
	for _, child := range n.children {
		if child.sample.max > 0 {
			children = append(children, child)
		}
	}
	sortNodes(children)
	return children
}

func sortNodes(nodes []*node) {
	sort.Slice(nodes, func(i, j int) bool {
		if nodes[i].sample.total == nodes[j].sample.total {
			if nodes[i].sample.max == nodes[j].sample.max {
				return nodes[i].name < nodes[j].name
			}
			return nodes[i].sample.max > nodes[j].sample.max
		}
		return nodes[i].sample.total > nodes[j].sample.total
	})
}

func writeNode(w io.Writer, n *node, prefix string, root bool) {
	label := n.name
	nextPrefix := ""
	if !root {
		label = prefix + "|- " + n.name
		nextPrefix = prefix + "|  "
	}
	average := n.sample.total / time.Duration(n.sample.count)
	fmt.Fprintf(w, "%-44s %7d %12s %12s %12s\n",
		label, n.sample.count, formatDuration(average),
		formatDuration(n.sample.max), formatDuration(n.sample.total))

	if root {
		nextPrefix = ""
	}
	for _, child := range sortedChildren(n) {
		writeNode(w, child, nextPrefix, false)
	}
}

func formatDuration(duration time.Duration) string {
	switch {
	case duration >= time.Second:
		return fmt.Sprintf("%.2f s", duration.Seconds())
	case duration >= time.Millisecond:
		return fmt.Sprintf("%.2f ms", float64(duration)/float64(time.Millisecond))
	default:
		return fmt.Sprintf("%.1f µs", float64(duration)/float64(time.Microsecond))
	}
}
