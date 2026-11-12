package updates_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/crazy-vedic/quark/internal/updates"
)

func TestLoadReleaseState_FileNotFound(t *testing.T) {
	tmpDir := t.TempDir()
	state, err := updates.LoadReleaseState(tmpDir)

	require.NoError(t, err)
	assert.Equal(t, updates.ReleaseState{}, state)
}

func TestLoadReleaseState_ValidFile(t *testing.T) {
	tmpDir := t.TempDir()
	stateDir := tmpDir
	statePath := filepath.Join(stateDir, "release-state.json")

	// Write a state file manually
	content := `{
  "last_checked_at": "2025-01-15T10:30:00Z",
  "next_attempt_at": "2025-01-15T22:30:00Z",
  "latest_version": "1.5.0",
  "last_error": ""
}`
	require.NoError(t, os.WriteFile(statePath, []byte(content), 0o600))

	state, err := updates.LoadReleaseState(tmpDir)
	require.NoError(t, err)

	assert.Equal(t, "1.5.0", state.LatestVersion)
	assert.Equal(t, "", state.LastError)
	assert.True(t, state.LastCheckedAt.Year() >= 2025)
	assert.True(t, state.NextAttemptAt.Year() >= 2025)
}

func TestLoadReleaseState_InvalidJSON(t *testing.T) {
	tmpDir := t.TempDir()
	statePath := filepath.Join(tmpDir, "release-state.json")

	require.NoError(t, os.WriteFile(statePath, []byte("invalid json"), 0o600))

	_, err := updates.LoadReleaseState(tmpDir)
	require.Error(t, err)
}

func TestSaveReleaseState_CreatesDirectory(t *testing.T) {
	tmpDir := t.TempDir()
	configDir := filepath.Join(tmpDir, "subdir", "deep")

	state := updates.ReleaseState{
		LatestVersion: "2.0.0",
	}

	err := updates.SaveReleaseState(configDir, state)
	require.NoError(t, err)

	// Verify directory was created
	info, err := os.Stat(configDir)
	require.NoError(t, err)
	assert.True(t, info.IsDir())

	// Verify file exists
	statePath := filepath.Join(configDir, "release-state.json")
	_, err = os.Stat(statePath)
	require.NoError(t, err)
}

func TestSaveReleaseState_RoundTrip(t *testing.T) {
	tmpDir := t.TempDir()

	now := time.Now().UTC()
	originalState := updates.ReleaseState{
		LastCheckedAt: now,
		NextAttemptAt: now.Add(12 * time.Hour),
		LatestVersion: "3.2.1",
		LastError:     "connection timeout",
	}

	err := updates.SaveReleaseState(tmpDir, originalState)
	require.NoError(t, err)

	loaded, err := updates.LoadReleaseState(tmpDir)
	require.NoError(t, err)

	assert.Equal(t, originalState.LatestVersion, loaded.LatestVersion)
	assert.Equal(t, originalState.LastError, loaded.LastError)
	// Times should be equal (within JSON marshaling precision)
	assert.WithinDuration(t, originalState.LastCheckedAt, loaded.LastCheckedAt, time.Second)
	assert.WithinDuration(t, originalState.NextAttemptAt, loaded.NextAttemptAt, time.Second)
}

func TestSaveReleaseState_Permissions(t *testing.T) {
	tmpDir := t.TempDir()

	state := updates.ReleaseState{LatestVersion: "1.0.0"}
	err := updates.SaveReleaseState(tmpDir, state)
	require.NoError(t, err)

	statePath := filepath.Join(tmpDir, "release-state.json")
	info, err := os.Stat(statePath)
	require.NoError(t, err)

	// File should be readable/writable only by owner (0o600)
	mode := info.Mode().Perm()
	assert.Equal(t, os.FileMode(0o600), mode)
}

func TestReleaseState_ShouldCheck(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name  string
		state updates.ReleaseState
		want  bool
	}{
		{
			name:  "zero next attempt at",
			state: updates.ReleaseState{NextAttemptAt: time.Time{}},
			want:  true,
		},
		{
			name:  "next attempt in past",
			state: updates.ReleaseState{NextAttemptAt: now.Add(-1 * time.Hour)},
			want:  true,
		},
		{
			name:  "next attempt in future",
			state: updates.ReleaseState{NextAttemptAt: now.Add(1 * time.Hour)},
			want:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.state.ShouldCheck()
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestReleaseState_WithSuccessfulCheck(t *testing.T) {
	before := time.Now()
	oldState := updates.ReleaseState{
		LatestVersion: "1.0.0",
		LastError:     "old error",
	}

	newState := oldState.WithSuccessfulCheck("2.0.0")
	after := time.Now()

	assert.Equal(t, "2.0.0", newState.LatestVersion)
	assert.Equal(t, "", newState.LastError)
	assert.True(t, newState.LastCheckedAt.After(before) || newState.LastCheckedAt.Equal(before))
	assert.True(t, newState.LastCheckedAt.Before(after) || newState.LastCheckedAt.Equal(after))

	// NextAttemptAt should be ~12 hours in the future
	expectedNextAttempt := time.Now().Add(12 * time.Hour)
	assert.WithinDuration(t, expectedNextAttempt, newState.NextAttemptAt, 2*time.Second)
}

func TestReleaseState_WithFailedCheck(t *testing.T) {
	oldState := updates.ReleaseState{
		LatestVersion: "2.0.0",
		LastCheckedAt: time.Now().Add(-24 * time.Hour),
	}

	testErr := "network error: connection refused"
	newState := oldState.WithFailedCheck(ErrTest(testErr))

	assert.Equal(t, "2.0.0", newState.LatestVersion)
	assert.Equal(t, testErr, newState.LastError)
	// LastCheckedAt should remain unchanged
	assert.Equal(t, oldState.LastCheckedAt, newState.LastCheckedAt)

	// NextAttemptAt should be ~1 hour in the future
	expectedNextAttempt := time.Now().Add(1 * time.Hour)
	assert.WithinDuration(t, expectedNextAttempt, newState.NextAttemptAt, 2*time.Second)
}

// ErrTest is a test error type for testing.
type ErrTest string

func (e ErrTest) Error() string {
	return string(e)
}
