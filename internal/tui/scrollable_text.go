package tui

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/crazy-vedic/quark/internal/timing"
)

// scrollableText is the read-only text component used for large bodies.
type scrollableText struct {
	content    string
	sourceKey  string
	formatted  bool
	offset     int
	wrapped    []string
	wrappedFor int
	debugLog   *os.File
	debugName  string
	timing     *timing.Collector
}

func (s *scrollableText) SetDebugLog(log *os.File, name string) {
	s.debugLog = log
	s.debugName = name
}

func (s *scrollableText) SetTiming(collector *timing.Collector) {
	s.timing = collector
}

func (s *scrollableText) logTiming(event string, started time.Time, width, height int, extra string) {
	if s.debugLog == nil {
		return
	}
	fmt.Fprintf(s.debugLog,
		"[%s] scrollable_%s component=%s duration=%s bytes=%d width=%d height=%d offset=%d wrapped_lines=%d %s\n",
		time.Now().Format("15:04:05.000"), event, s.debugName, time.Since(started),
		len(s.content), width, height, s.offset, len(s.wrapped), extra)
}

func (s *scrollableText) SetContent(content string) {
	if s.content != content {
		debugLog, debugName := s.debugLog, s.debugName
		*s = scrollableText{content: content, debugLog: debugLog, debugName: debugName}
	}
}

// SetFormattedContent caches the result of format for a logical source. This
// keeps expensive formatting (for example JSON pretty-printing and syntax
// highlighting) out of repeated scroll/update/render cycles.
func (s *scrollableText) SetFormattedContent(sourceKey string, format func() string) {
	if s.formatted && s.sourceKey == sourceKey {
		return
	}
	content := ""
	if format != nil {
		stop := s.timing.Track("scrollable.format." + s.debugName)
		defer stop()
		content = format()
	}
	debugLog, debugName := s.debugLog, s.debugName
	*s = scrollableText{
		content:   content,
		sourceKey: sourceKey,
		formatted: true,
		debugLog:  debugLog,
		debugName: debugName,
	}
}

func (s *scrollableText) lines(width int) []string {
	if width <= 0 || s.content == "" {
		return nil
	}
	if s.wrapped == nil || s.wrappedFor != width {
		started := time.Now()
		stop := s.timing.Track("scrollable.wrap." + s.debugName)
		defer stop()
		s.wrapped = wrappedTextLines(s.content, width)
		s.wrappedFor = width
		s.logTiming("wrap", started, width, 0, "cache=miss")
	}
	return s.wrapped
}

func (s *scrollableText) Scroll(delta, width, height int) {
	started := time.Now()
	stop := s.timing.Track("scrollable.scroll." + s.debugName)
	defer stop()
	if width <= 0 || height <= 0 {
		s.logTiming("scroll", started, width, height, fmt.Sprintf("delta=%d result=invalid_viewport", delta))
		return
	}
	lines := s.lines(width)
	maxOffset := len(lines) - height
	if maxOffset < 0 {
		maxOffset = 0
	}
	s.offset += delta
	if s.offset < 0 {
		s.offset = 0
	}
	if s.offset > maxOffset {
		s.offset = maxOffset
	}
	s.logTiming("scroll", started, width, height, fmt.Sprintf("delta=%d max_offset=%d", delta, maxOffset))
}

func (s scrollableText) View(width, height int) string {
	started := time.Now()
	stop := s.timing.Track("scrollable.view." + s.debugName)
	defer stop()
	if width <= 0 || height <= 0 {
		s.logTiming("view", started, width, height, "result=invalid_viewport")
		return ""
	}
	lines := s.lines(width)
	if len(lines) == 0 {
		s.logTiming("view", started, width, height, "result=empty")
		return ""
	}
	offset := s.offset
	maxOffset := len(lines) - height
	if offset > maxOffset {
		offset = maxOffset
	}
	if offset < 0 {
		offset = 0
	}
	end := offset + height
	if end > len(lines) {
		end = len(lines)
	}
	view := strings.Join(lines[offset:end], "\n")
	s.logTiming("view", started, width, height, fmt.Sprintf("visible_lines=%d", end-offset))
	return view
}

func wrappedTextLines(content string, width int) []string {
	if content == "" || width <= 0 {
		return nil
	}
	raw := strings.Split(softWrap(content, width), "\n")
	for i := range raw {
		raw[i] = strings.TrimRight(raw[i], " ")
	}
	return raw
}
