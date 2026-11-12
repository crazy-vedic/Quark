package updates

import (
	"testing"
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
