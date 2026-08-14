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
	offset    int
	cache     *scrollableTextCache
	debugLog  *os.File
	debugName string
	timing    *timing.Collector
}

// scrollableTextCache contains derived data only. It is shared across model
// copies because Bubble Tea models use value semantics, while offset remains
// on scrollableText as user interaction state.
type scrollableTextCache struct {
	content   string
	sourceKey string
	formatted bool
	wrapped   map[int][]string
}

func (s *scrollableText) ensureCache() *scrollableTextCache {
	if s.cache == nil {
		s.cache = &scrollableTextCache{wrapped: make(map[int][]string)}
	} else if s.cache.wrapped == nil {
		s.cache.wrapped = make(map[int][]string)
	}
	return s.cache
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
	cache := s.ensureCache()
	wrappedLines := len(cache.wrapped[width])
	fmt.Fprintf(s.debugLog,
		"[%s] scrollable_%s component=%s duration=%s bytes=%d width=%d height=%d offset=%d wrapped_lines=%d cached_widths=%d %s\n",
		time.Now().Format("15:04:05.000"), event, s.debugName, time.Since(started),
		len(cache.content), width, height, s.offset, wrappedLines, len(cache.wrapped), extra)
}

func (s *scrollableText) SetContent(content string) {
	cache := s.ensureCache()
	if cache.content != content || cache.formatted {
		changed := cache.content != content
		*cache = scrollableTextCache{content: content, wrapped: make(map[int][]string)}
		if changed {
			s.offset = 0
		}
	}
}

// SetFormattedContent caches the result of format for a logical source. This
// keeps expensive formatting (for example JSON pretty-printing and syntax
// highlighting) out of repeated scroll/update/render cycles.
func (s *scrollableText) SetFormattedContent(sourceKey string, format func() string, parent ...*timing.Span) {
	cache := s.ensureCache()
	if cache.formatted && cache.sourceKey == sourceKey {
		return
	}
	content := ""
	if format != nil {
		span := s.timing.Track("scrollable.format."+s.debugName, timingParent(parent))
		defer span.Done()
		content = format()
	}
	*cache = scrollableTextCache{
		content: content, sourceKey: sourceKey, formatted: true,
		wrapped: make(map[int][]string),
	}
	s.offset = 0
}

func (s *scrollableText) lines(width int, parent ...*timing.Span) []string {
	cache := s.ensureCache()
	if width <= 0 || cache.content == "" {
		return nil
	}
	if _, ok := cache.wrapped[width]; !ok {
		started := time.Now()
		span := s.timing.Track("scrollable.wrap."+s.debugName, timingParent(parent))
		defer span.Done()
		cache.wrapped[width] = wrappedTextLines(cache.content, width)
		s.logTiming("wrap", started, width, 0, "cache=miss")
	}
	return cache.wrapped[width]
}

func (s *scrollableText) Scroll(delta, width, height int, parent ...*timing.Span) {
	started := time.Now()
	span := s.timing.Track("scrollable.scroll."+s.debugName, timingParent(parent))
	defer span.Done()
	if width <= 0 || height <= 0 {
		s.logTiming("scroll", started, width, height, fmt.Sprintf("delta=%d result=invalid_viewport", delta))
		return
	}
	lines := s.lines(width, span)
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

func (s scrollableText) View(width, height int, parent ...*timing.Span) string {
	started := time.Now()
	span := s.timing.Track("scrollable.view."+s.debugName, timingParent(parent))
	defer span.Done()
	if width <= 0 || height <= 0 {
		s.logTiming("view", started, width, height, "result=invalid_viewport")
		return ""
	}
	lines := s.lines(width, span)
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

func timingParent(parent []*timing.Span) *timing.Span {
	if len(parent) == 0 {
		return nil
	}
	return parent[0]
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
