// Package timing provides opt-in wall-clock timing for long-running paths.
package timing

import (
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"sync"
	"time"
)

type sample struct {
	count int
	total time.Duration
	max   time.Duration
}

// Collector aggregates timing samples by operation name.
type Collector struct {
	enabled bool
	mu      sync.Mutex
	samples map[string]sample
}

func New(enabled bool) *Collector {
	return &Collector{enabled: enabled, samples: make(map[string]sample)}
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

// Track starts an operation and returns a function intended for defer.
func (c *Collector) Track(operation string) func() {
	if !c.Enabled() {
		return func() {}
	}
	started := time.Now()
	return func() { c.record(operation, time.Since(started)) }
}

func (c *Collector) record(operation string, duration time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	value := c.samples[operation]
	value.count++
	value.total += duration
	if duration > value.max {
		value.max = duration
	}
	c.samples[operation] = value
}

type reportRow struct {
	operation string
	sample
}

const maxReportRows = 30

// Report writes operations ordered by cumulative total duration. Reports are
// capped at the top 30 operations, or fewer when fewer samples exist.
func (c *Collector) Report(w io.Writer, limit int) {
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
