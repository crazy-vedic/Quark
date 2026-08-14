package tui

import (
	"fmt"
	"os"
	"time"
)

// logDebugTiming records elapsed time for expensive UI paths when --debug is
// enabled. It intentionally does nothing in normal runs.
func logDebugTiming(log *os.File, operation string, started time.Time, details string) {
	if log == nil {
		return
	}
	fmt.Fprintf(log, "[%s] timing operation=%s duration=%s %s\n",
		time.Now().Format("15:04:05.000"), operation, time.Since(started), details)
}
