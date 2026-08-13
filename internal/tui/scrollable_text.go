package tui

import "strings"

// scrollableText is the read-only text component used for large bodies.
type scrollableText struct {
	content string
	offset  int
}

func (s *scrollableText) SetContent(content string) {
	if s.content != content {
		s.content = content
		s.offset = 0
	}
}

func (s *scrollableText) Scroll(delta, width, height int) {
	if width <= 0 || height <= 0 {
		return
	}
	lines := wrappedTextLines(s.content, width)
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
}

func (s scrollableText) View(width, height int) string {
	if width <= 0 || height <= 0 {
		return ""
	}
	lines := wrappedTextLines(s.content, width)
	if len(lines) == 0 {
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
	return strings.Join(lines[offset:end], "\n")
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
