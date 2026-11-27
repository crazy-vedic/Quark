package textedit

import (
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/crazy-vedic/quark/internal/keybindings"
)

func TestKeymapFromConfigOverridesSetAction(t *testing.T) {
	cfg := keybindings.DefaultKeybindings()
	cfg.SelectAll = "ctrl+y"
	k := KeymapFromConfigFor(runtime.GOOS, cfg)
	assert.Equal(t, Binding{"ctrl+y"}, k.SelectAll)
}

func TestKeymapFromConfigUnsetKeepsDefault(t *testing.T) {
	k := KeymapFromConfigFor(runtime.GOOS, keybindings.Keybindings{})
	assert.Equal(t, DefaultKeymap(), k)
}

func TestKeymapFromConfigDefaultKeepsEditorBindings(t *testing.T) {
	k := KeymapFromConfigFor(runtime.GOOS, keybindings.DefaultKeybindings())
	assert.Equal(t, DefaultKeymap().SelectWordLeft, k.SelectWordLeft)
	assert.True(t, k.SelectWordLeft.Match("ctrl+shift+left"))
	assert.True(t, k.SelectWordRight.Match("ctrl+shift+right"))
}

func TestDefaultKeymapFindOpenIsCmdOnlyOnMacOS(t *testing.T) {
	assert.Equal(t, Binding{"ctrl+f", "cmd+f"}, DefaultKeymapFor("darwin").FindOpen)
	for _, goos := range []string{"windows", "linux", "freebsd"} {
		k := DefaultKeymapFor(goos)
		assert.Equal(t, Binding{"ctrl+f"}, k.FindOpen, goos)
		assert.False(t, k.FindOpen.Match("cmd+f"), goos)
	}
}

func TestKeymapFromConfigFindOpenFollowsOS(t *testing.T) {
	k := KeymapFromConfigFor("windows", keybindings.DefaultKeybindings())
	assert.False(t, k.FindOpen.Match("cmd+f"))
	assert.True(t, k.FindOpen.Match("ctrl+f"))
}
