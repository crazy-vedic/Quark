package timing

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func TestCollectorReportsLongestOperations(t *testing.T) {
	c := New(true)
	stop := c.Track("fast")
	stop()
	stop = c.Track("slow")
	time.Sleep(2 * time.Millisecond)
	stop()

	var out bytes.Buffer
	c.Report(&out, 1)
	result := out.String()
	if !strings.Contains(result, "=== Quark performance: slowest 1 operations ===") {
		t.Fatalf("report header missing: %s", result)
	}
	if !strings.Contains(result, "slow") || strings.Contains(result, "fast") {
		t.Fatalf("report did not select slowest operation: %s", result)
	}
	if !strings.Contains(result, "Average") || !strings.Contains(result, "Max") {
		t.Fatalf("report columns are not human-readable: %s", result)
	}
}

func TestDisabledCollectorDoesNothing(t *testing.T) {
	c := New(false)
	c.Track("ignored")()
	var out bytes.Buffer
	c.Report(&out, 10)
	if out.Len() != 0 {
		t.Fatalf("disabled collector wrote report: %q", out.String())
	}
}

func TestCollectorSortsByTotalAndCapsAtThirty(t *testing.T) {
	c := New(true)
	c.record("low-total", 1*time.Millisecond)
	c.record("high-total", 3*time.Millisecond)
	c.record("high-total", 3*time.Millisecond)
	for i := 0; i < 35; i++ {
		c.record("operation-"+string(rune('a'+i)), time.Microsecond)
	}

	var out bytes.Buffer
	c.Report(&out, 100)
	result := out.String()
	if !strings.Contains(result, "slowest 30 operations") {
		t.Fatalf("report was not capped at 30: %s", result)
	}
	if !strings.Contains(result, "high-total") {
		t.Fatalf("highest-total operation missing: %s", result)
	}
	if strings.Index(result, "high-total") > strings.Index(result, "low-total") {
		t.Fatalf("report is not sorted by total duration: %s", result)
	}
}
