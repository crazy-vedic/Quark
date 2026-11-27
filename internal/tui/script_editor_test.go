package tui

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestScriptEditorStashesTheShownPhase(t *testing.T) {
	e := scriptEditor{area: newScriptArea(), pre: "old pre", post: "old post"}
	e.area.SetValue("new pre")
	e.stashScriptPhase()
	assert.Equal(t, "new pre", e.pre)
	assert.Equal(t, "old post", e.post)

	m := Model{scripts: e}
	m = m.switchScriptPhase()
	assert.Equal(t, scriptPhasePost, m.scripts.phase)
	assert.Equal(t, "old post", m.scripts.area.Value())

	m = m.switchScriptPhase()
	assert.Equal(t, scriptPhasePre, m.scripts.phase)
	assert.Equal(t, "new pre", m.scripts.area.Value())
}
