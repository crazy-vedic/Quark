package keybindings

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

var hintAliases = map[string][]string{
	ActionSendRequest: {KeyAltEnter},
	"cert_down":       {"down"},
	"cert_up":         {"up"},
	"cert_edit":       {KeyEnter},
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

// GOOSDarwin is the runtime.GOOS value for macOS. Only macOS labels keys with
// the Command (⌘) and Option (⌥) symbols.
const GOOSDarwin = "darwin"

// FormatKey renders an internal key string in the compact UI format for the
// platform goos (a runtime.GOOS value). On macOS, cmd and alt render as ⌘ and
// ⌥; elsewhere they render as Cmd and Alt, and ⌘ is never shown.
func FormatKey(goos, key string) string {
	key = strings.TrimSpace(key)
	if key == "" {
		return ""
	}

	switch strings.ToLower(key) {
	case KeyEnter:
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
	case KeyLeft:
		return "←"
	case KeyRight:
		return "→"
	case "shift+left":
		return "Shift+←"
	case "shift+right":
		return "Shift+→"
	case KeyAltEnter:
		// Terminals on macOS send Cmd+Enter as alt+enter.
		if goos == GOOSDarwin {
			return "⌘+Enter"
		}
	}

	if !strings.Contains(key, "+") && utf8.RuneCountInString(key) == 1 {
		return key
	}

	parts := strings.Split(key, "+")
	for i, part := range parts {
		parts[i] = formatKeyPart(goos, part)
	}
	return strings.Join(parts, "+")
}

func formatKeyPart(goos, part string) string {
	darwin := goos == GOOSDarwin
	switch strings.ToLower(part) {
	case "ctrl":
		return "Ctrl"
	case "shift":
		return "Shift"
	case "alt":
		if darwin {
			return "⌥"
		}
		return "Alt"
	case "cmd":
		if darwin {
			return "⌘"
		}
		return "Cmd"
	case "up":
		return "↑"
	case "down":
		return "↓"
	case KeyLeft:
		return "←"
	case KeyRight:
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
