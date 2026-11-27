package cli

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/crazy-vedic/quark/internal/runner"
)

func TestTestCmdRejectsUnknownFormatBeforeRunning(t *testing.T) {
	cmd := NewTestCmd(nil, nil)
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"API", "--format", "yaml"})
	err := cmd.Execute()
	assert.ErrorContains(t, err, `unknown format "yaml"`)
}

func TestTestCmdDefaults(t *testing.T) {
	cmd := NewTestCmd(nil, nil)
	format, err := cmd.Flags().GetString("format")
	assert.NoError(t, err)
	assert.Equal(t, runner.FormatText, format)
	bail, err := cmd.Flags().GetBool("bail")
	assert.NoError(t, err)
	assert.False(t, bail)
}
