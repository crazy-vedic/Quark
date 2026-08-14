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
	stop.Done()
	stop = c.Track("slow")
	time.Sleep(2 * time.Millisecond)
	stop.Done()

	var out bytes.Buffer
	c.ReportFlat(&out, 1)
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
	c.Track("ignored").Done()
	var out bytes.Buffer
	c.Report(&out, 10)
	if out.Len() != 0 {
		t.Fatalf("disabled collector wrote report: %q", out.String())
	}
}

func TestCollectorReportsTreeRelationships(t *testing.T) {
	c := New(true)
	parent := c.Track("FuncF")
	childA := c.Track("FuncA", parent)
	childB := c.Track("FuncB", childA)
	time.Sleep(time.Millisecond)
	childB.Done()
	childA.Done()
	childC := c.Track("FuncC", parent)
	childD := c.Track("FuncD", childC)
	time.Sleep(time.Microsecond)
	childD.Done()
	childC.Done()
	parent.Done()
	independent := c.Track("FuncE")
	time.Sleep(time.Microsecond)
	independent.Done()

	var out bytes.Buffer
	c.ReportTree(&out, 30)
	result := out.String()
	if !strings.Contains(result, "=== Quark timing tree: slowest 2 root operations ===") {
		t.Fatalf("tree header missing: %s", result)
	}
	for _, want := range []string{"FuncF", "|- FuncA", "|  |- FuncB", "|- FuncC", "|  |- FuncD", "FuncE"} {
		if !strings.Contains(result, want) {
			t.Fatalf("tree report missing %q: %s", want, result)
		}
	}
	if strings.Index(result, "FuncF") > strings.Index(result, "FuncE") {
		t.Fatalf("tree roots are not sorted by total duration: %s", result)
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
	c.ReportFlat(&out, 100)
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

func TestParseReportFormat(t *testing.T) {
	tests := map[string]ReportFormat{
		"":       ReportFormatTree,
		"tree":   ReportFormatTree,
		"flat":   ReportFormatFlat,
		"off":    ReportFormatOff,
		" FLAT ": ReportFormatFlat,
	}
	for input, want := range tests {
		got, err := ParseReportFormat(input)
		if err != nil {
			t.Fatalf("ParseReportFormat(%q) returned error: %v", input, err)
		}
		if got != want {
			t.Fatalf("ParseReportFormat(%q) = %q, want %q", input, got, want)
		}
	}
	if _, err := ParseReportFormat("table"); err == nil {
		t.Fatal("ParseReportFormat accepted invalid format")
	}
}
