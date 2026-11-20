package updates

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNpmSource_UpgradeCommand(t *testing.T) {
	source := NewNpmSource("@quark-client/quark")
	cmd := source.UpgradeCommand()
	expected := "npm i -g @quark-client/quark@latest"
	if cmd != expected {
		t.Errorf("expected %q, got %q", expected, cmd)
	}
}

func TestNpmSource_DowngradeWarning(t *testing.T) {
	source := NewNpmSource("@quark-client/quark")
	warning := source.DowngradeWarning("v1.2.0", "v1.1.0")
	expected := "downgrading from v1.2.0 to v1.1.0"
	if warning != expected {
		t.Errorf("expected %q, got %q", expected, warning)
	}
}

// Windows paths use backslashes and are compared case-insensitively. The
// check used "/" as the separator, so C:\Program Files was never a system path.
func TestIsSystemPath_WindowsPaths(t *testing.T) {
	for _, path := range []string{
		`C:\Program Files\Quark\quark.exe`,
		`c:\program files\quark\quark.exe`,
	} {
		assert.True(t, isSystemPathFor(path, "windows"), path)
	}
	assert.False(t, isSystemPathFor(`C:\Users\me\bin\quark.exe`, "windows"))
	assert.False(t, isSystemPathFor(`C:\Program Files Extra\quark.exe`, "windows"),
		"a sibling directory with a longer name must not match")
}
