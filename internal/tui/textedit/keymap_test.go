package textedit

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/crazy-vedic/quark/internal/keybindings"
)

func TestKeymapFromConfigOverridesSetAction(t *testing.T) {
	cfg := keybindings.DefaultKeybindings()
	cfg.SelectAll = "ctrl+y"
	k := KeymapFromConfig(cfg)
	assert.Equal(t, Binding{"ctrl+y"}, k.SelectAll)
}

func TestKeymapFromConfigUnsetKeepsDefault(t *testing.T) {
	k := KeymapFromConfig(keybindings.Keybindings{})
	assert.Equal(t, DefaultKeymap(), k)
}

func TestKeymapFromConfigDefaultKeepsEditorBindings(t *testing.T) {
	k := KeymapFromConfig(keybindings.DefaultKeybindings())
	assert.Equal(t, DefaultKeymap().SelectWordLeft, k.SelectWordLeft)
	assert.True(t, k.SelectWordLeft.Match("ctrl+shift+left"))
	assert.True(t, k.SelectWordRight.Match("ctrl+shift+right"))
}
