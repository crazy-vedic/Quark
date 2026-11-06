package updates_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/crazy-vedic/quark/internal/updates"
)

// mockSource is a test implementation of the Source interface.
type mockSource struct {
	latestVersion  string
	latestErr      error
	upgradeCmd     string
	downgradeMsg   string
}

func (m *mockSource) LatestVersion(ctx context.Context) (string, error) {
	return m.latestVersion, m.latestErr
}

func (m *mockSource) IsUpdateAvailable(ctx context.Context, current string) (bool, string, error) {
	if m.latestErr != nil {
		return false, "", m.latestErr
	}
	cmp, err := updates.CompareVersions(current, m.latestVersion)
	if err != nil {
		return false, "", err
	}
	return cmp < 0, m.latestVersion, nil
}

func (m *mockSource) UpgradeCommand() string {
	return m.upgradeCmd
}

func (m *mockSource) DowngradeWarning(current, target string) string {
	return m.downgradeMsg
}

func TestVersionCheckResult_StatusMessage_Error(t *testing.T) {
	result := &updates.VersionCheckResult{
		Error: errors.New("network timeout"),
	}

	msg := result.StatusMessage()
	assert.Equal(t, "update check failed: network timeout", msg)
}

func TestVersionCheckResult_StatusMessage_UpToDate(t *testing.T) {
	result := &updates.VersionCheckResult{
		Channel:         updates.ChannelBrew,
		UpdateAvailable: false,
	}

	msg := result.StatusMessage()
	assert.Equal(t, "up to date (via brew)", msg)
}

func TestVersionCheckResult_StatusMessage_UpdateAvailableWithSelfUpdate(t *testing.T) {
	result := &updates.VersionCheckResult{
		Channel:         updates.ChannelBinary,
		Latest:          "2.0.0",
		UpdateAvailable: true,
		CanSelfUpdate:   true,
	}

	msg := result.StatusMessage()
	assert.Equal(t, "update available: 2.0.0 (run: quark update)", msg)
}

func TestVersionCheckResult_StatusMessage_UpdateAvailableRequiresCommand(t *testing.T) {
	result := &updates.VersionCheckResult{
		Channel:         updates.ChannelBrew,
		Latest:          "2.0.0",
		UpdateAvailable: true,
		CanSelfUpdate:   false,
		UpgradeCommand:  "brew upgrade quark",
	}

	msg := result.StatusMessage()
	assert.Equal(t, "update available: 2.0.0 (run: brew upgrade quark)", msg)
}

func TestVersionCheckResult_StatusBarText_NoUpdate(t *testing.T) {
	result := &updates.VersionCheckResult{
		UpdateAvailable: false,
	}

	text := result.StatusBarText()
	assert.Equal(t, "", text)
}

func TestVersionCheckResult_StatusBarText_UpdateWithSelfUpdate(t *testing.T) {
	result := &updates.VersionCheckResult{
		Latest:          "1.5.0",
		UpdateAvailable: true,
		CanSelfUpdate:   true,
	}

	text := result.StatusBarText()
	assert.Equal(t, "↑ 1.5.0 · quark update", text)
}

func TestVersionCheckResult_StatusBarText_UpdateWithCommand(t *testing.T) {
	result := &updates.VersionCheckResult{
		Latest:          "1.5.0",
		UpdateAvailable: true,
		CanSelfUpdate:   false,
		UpgradeCommand:  "brew upgrade quark",
	}

	text := result.StatusBarText()
	assert.Equal(t, "↑ 1.5.0 · brew upgrade quark", text)
}

func TestMockSource_LatestVersion(t *testing.T) {
	source := &mockSource{latestVersion: "2.0.0"}

	ver, err := source.LatestVersion(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "2.0.0", ver)
}

func TestMockSource_LatestVersion_Error(t *testing.T) {
	testErr := errors.New("network error")
	source := &mockSource{latestErr: testErr}

	_, err := source.LatestVersion(context.Background())
	assert.Equal(t, testErr, err)
}

func TestMockSource_IsUpdateAvailable(t *testing.T) {
	tests := []struct {
		name    string
		current string
		latest  string
		want    bool
	}{
		{name: "update available", current: "1.0.0", latest: "2.0.0", want: true},
		{name: "already latest", current: "2.0.0", latest: "2.0.0", want: false},
		{name: "newer installed", current: "3.0.0", latest: "2.0.0", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			source := &mockSource{latestVersion: tt.latest}
			available, latest, err := source.IsUpdateAvailable(context.Background(), tt.current)

			require.NoError(t, err)
			assert.Equal(t, tt.want, available)
			assert.Equal(t, tt.latest, latest)
		})
	}
}

func TestMockSource_IsUpdateAvailable_Error(t *testing.T) {
	testErr := errors.New("check failed")
	source := &mockSource{latestErr: testErr}

	_, _, err := source.IsUpdateAvailable(context.Background(), "1.0.0")
	assert.Equal(t, testErr, err)
}

func TestMockSource_UpgradeCommand(t *testing.T) {
	source := &mockSource{upgradeCmd: "apt upgrade quark"}

	cmd := source.UpgradeCommand()
	assert.Equal(t, "apt upgrade quark", cmd)
}

func TestMockSource_DowngradeWarning(t *testing.T) {
	source := &mockSource{downgradeMsg: "warning: downgrading"}

	msg := source.DowngradeWarning("2.0.0", "1.0.0")
	assert.Equal(t, "warning: downgrading", msg)
}

func TestVersionCheckResult_Timing(t *testing.T) {
	now := time.Now()
	result := &updates.VersionCheckResult{
		CheckedAt: now,
	}

	assert.WithinDuration(t, now, result.CheckedAt, time.Second)
}
