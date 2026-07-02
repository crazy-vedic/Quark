package keybindings

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

var hintAliases = map[string][]string{
	ActionSendRequest: {"alt+enter"},
}

// HintKeys returns the configured key for an action plus any user-facing
// aliases that should be surfaced in inline hints.
func HintKeys(binds Keybindings, action string, includeAliases bool) []string {
	keys := []string{GetAction(binds, action)}
	if includeAliases {
		keys = append(keys, hintAliases[action]...)
	}
	return dedupeNonEmpty(keys)
}

// FormatKey renders an internal key string in the compact UI format.
func FormatKey(key string) string {
	key = strings.TrimSpace(key)
	if key == "" {
		return ""
	}

	switch strings.ToLower(key) {
	case "enter":
		return "Enter"
	case "esc":
		return "Esc"
	case "tab":
		return "Tab"
	case KeyShiftTab:
		return "Shift+Tab"
	case "up":
		return "↑"
	case "down":
		return "↓"
	case "left":
		return "←"
	case "right":
		return "→"
	case "shift+left":
		return "Shift+←"
	case "shift+right":
		return "Shift+→"
	case "alt+enter":
		return "⌘+Enter"
	}

	if !strings.Contains(key, "+") && utf8.RuneCountInString(key) == 1 {
		return key
	}

	parts := strings.Split(key, "+")
	for i, part := range parts {
		parts[i] = formatKeyPart(part)
	}
	return strings.Join(parts, "+")
}

func formatKeyPart(part string) string {
	switch strings.ToLower(part) {
	case "ctrl":
		return "Ctrl"
	case "shift":
		return "Shift"
	case "alt":
		return "Alt"
	case "cmd":
		return "⌘"
	case "up":
		return "↑"
	case "down":
		return "↓"
	case "left":
		return "←"
	case "right":
		return "→"
	case "enter":
		return "Enter"
	case "esc":
		return "Esc"
	case "tab":
		return "Tab"
	}

	if utf8.RuneCountInString(part) == 1 {
		return strings.ToUpper(part)
	}

	var out []rune
	for i, r := range part {
		if i == 0 {
			out = append(out, unicode.ToUpper(r))
			continue
		}
		out = append(out, r)
	}
	return string(out)
}

func dedupeNonEmpty(keys []string) []string {
	seen := make(map[string]struct{}, len(keys))
	out := make([]string, 0, len(keys))
	for _, key := range keys {
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, key)
	}
	return out
}
