package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

// DebugLogger writes timestamped lines to a file when debug mode is enabled.
// If out is nil, Logf is a no-op: zero overhead when --debug is not passed.
// Safe for concurrent use.
type DebugLogger struct {
	mu  sync.Mutex
	out *os.File
}

// NewDebugLogger creates a logger. out may be nil.
func NewDebugLogger(out *os.File) *DebugLogger {
	return &DebugLogger{out: out}
}

// SetFile updates the underlying file. Safe for concurrent use.
func (l *DebugLogger) SetFile(out *os.File) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.out = out
}

// Logf writes a timestamped line with the caller function name if the logger is enabled.
func (l *DebugLogger) Logf(format string, args ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.out == nil {
		return
	}

	caller := "unknown:0:unknown"
	if pc, file, line, ok := runtime.Caller(1); ok {
		fnName := "unknown"
		if fn := runtime.FuncForPC(pc); fn != nil {
			fnName = fn.Name()
			if i := strings.LastIndex(fnName, "/"); i != -1 {
				fnName = fnName[i+1:]
			}
			if i := strings.Index(fnName, "."); i != -1 {
				fnName = fnName[i+1:]
			}
		}
		caller = fmt.Sprintf("%s:%d:%s", filepath.Base(file), line, fnName)
	}

	ts := time.Now().Format(time.RFC3339)
	line := fmt.Sprintf("[debug] %s %s "+format+"\n", append([]any{ts, caller}, args...)...)
	_, _ = l.out.WriteString(line)
}
