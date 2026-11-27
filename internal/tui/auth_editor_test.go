package tui

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/crazy-vedic/quark/internal/domain"
)

func TestAuthEditor_InheritIsLastInTheCycle(t *testing.T) {
	ed := newAuthEditor(&domain.Request{AuthType: domain.AuthTypeAPIKey})
	ed.cycleType(1)
	assert.Equal(t, domain.AuthTypeInherit, ed.authType)
	assert.Equal(t, "Inherit", ed.summary())
	assert.Empty(t, ed.rows()[1:], "inherit has no fields of its own")

	ed.cycleType(1)
	assert.Equal(t, domain.AuthTypeNone, ed.authType)
}
