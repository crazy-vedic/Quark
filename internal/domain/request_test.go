package domain

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRequest_CheckEnabled(t *testing.T) {
	assert.NoError(t, (&Request{Name: "on"}).CheckEnabled(), "the zero value is enabled")
	assert.NoError(t, (*Request)(nil).CheckEnabled(), "a nil request is not disabled")

	err := (&Request{Name: "off", Disabled: true}).CheckEnabled()
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrRequestDisabled))
	assert.Contains(t, err.Error(), `"off"`)
}
