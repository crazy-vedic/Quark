package keybindings

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestFormatKeyLabelsFollowOS(t *testing.T) {
	tests := []struct {
		goos, key, want string
	}{
		{"darwin", "alt+enter", "⌘+Enter"},
		{"windows", "alt+enter", "Alt+Enter"},
		{"linux", "alt+enter", "Alt+Enter"},
		{"darwin", "alt+left", "⌥+←"},
		{"linux", "alt+left", "Alt+←"},
		{"darwin", "cmd+f", "⌘+F"},
		{"linux", "cmd+f", "Cmd+F"},
		{"windows", "ctrl+s", "Ctrl+S"},
		{"linux", "shift+tab", "Shift+Tab"},
		{"darwin", "enter", "Enter"},
		{"linux", "q", "q"},
		{"linux", "", ""},
	}
	for _, tt := range tests {
		got := FormatKey(tt.goos, tt.key)
		assert.Equal(t, tt.want, got, "%s %q", tt.goos, tt.key)
		if tt.goos != GOOSDarwin {
			assert.NotContains(t, got, "⌘",
				"%s %q must never show the Command symbol", tt.goos, tt.key)
		}
	}
}
