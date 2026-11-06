package updates_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/crazy-vedic/quark/internal/updates"
)

func TestCompareVersions(t *testing.T) {
	tests := []struct {
		name    string
		v1      string
		v2      string
		want    int
		wantErr bool
	}{
		// Exact matches
		{name: "equal versions", v1: "1.0.0", v2: "1.0.0", want: 0},
		{name: "equal with leading v", v1: "v1.0.0", v2: "v1.0.0", want: 0},

		// Major version differences
		{name: "v1 major greater", v1: "2.0.0", v2: "1.0.0", want: 1},
		{name: "v1 major less", v1: "1.0.0", v2: "2.0.0", want: -1},

		// Minor version differences
		{name: "v1 minor greater", v1: "1.1.0", v2: "1.0.0", want: 1},
		{name: "v1 minor less", v1: "1.0.0", v2: "1.1.0", want: -1},

		// Patch version differences
		{name: "v1 patch greater", v1: "1.0.1", v2: "1.0.0", want: 1},
		{name: "v1 patch less", v1: "1.0.0", v2: "1.0.1", want: -1},

		// Pre-release versions
		{name: "stable > prerelease", v1: "1.0.0", v2: "1.0.0-alpha", want: 1},
		{name: "prerelease < stable", v1: "1.0.0-beta", v2: "1.0.0", want: -1},
		{name: "alpha < beta", v1: "1.0.0-alpha", v2: "1.0.0-beta", want: -1},
		{name: "alpha.1 < alpha.2", v1: "1.0.0-alpha.1", v2: "1.0.0-alpha.2", want: -1},

		// Complex versions
		{name: "with build metadata", v1: "1.0.0+build1", v2: "1.0.0+build2", want: 0},
		{name: "complex comparison", v1: "1.2.3-rc.1", v2: "1.2.3", want: -1},

		// Invalid versions
		{name: "invalid v1", v1: "not-a-version", v2: "1.0.0", wantErr: true},
		{name: "invalid v2", v1: "1.0.0", v2: "not-a-version", wantErr: true},
		{name: "both invalid", v1: "bad", v2: "bad", wantErr: true},
		{name: "empty v1", v1: "", v2: "1.0.0", wantErr: true},
		{name: "empty v2", v1: "1.0.0", v2: "", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := updates.CompareVersions(tt.v1, tt.v2)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestIsPrerelease(t *testing.T) {
	tests := []struct {
		name string
		v    string
		want bool
	}{
		{name: "stable", v: "1.0.0", want: false},
		{name: "alpha", v: "1.0.0-alpha", want: true},
		{name: "beta", v: "1.0.0-beta", want: true},
		{name: "rc", v: "1.0.0-rc.1", want: true},
		{name: "with build metadata", v: "1.0.0+build", want: false},
		{name: "prerelease with build", v: "1.0.0-alpha+build", want: true},

		// Invalid versions default to not prerelease
		{name: "invalid version", v: "not-a-version", want: false},
		{name: "empty version", v: "", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := updates.IsPrerelease(tt.v)
			assert.Equal(t, tt.want, got)
		})
	}
}
