package updates

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/crazy-vedic/quark/internal/atomicfile"
)

// ReleaseState tracks the last known update status and timing.
type ReleaseState struct {
	LastCheckedAt time.Time `json:"last_checked_at"`
	NextAttemptAt time.Time `json:"next_attempt_at"`
	LatestVersion string    `json:"latest_version,omitempty"`
	LastError     string    `json:"last_error,omitempty"`
}

// LoadReleaseState reads the release-state.json file from the config directory.
// Returns an empty state if the file doesn't exist.
func LoadReleaseState(configDir string) (ReleaseState, error) {
	path := filepath.Join(configDir, "release-state.json")
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return ReleaseState{}, nil
		}
		return ReleaseState{}, err
	}

	var state ReleaseState
	if err := json.Unmarshal(data, &state); err != nil {
		return ReleaseState{}, err
	}
	return state, nil
}

// SaveReleaseState writes the release-state.json file to the config directory.
func SaveReleaseState(configDir string, state ReleaseState) error {
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		return err
	}

	path := filepath.Join(configDir, "release-state.json")
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}

	return atomicfile.WriteFile(path, data, 0o600)
}

// ShouldCheck returns true if the next check should be attempted.
// Returns true if NextAttemptAt is zero or has passed.
func (s ReleaseState) ShouldCheck() bool {
	return s.NextAttemptAt.IsZero() || time.Now().After(s.NextAttemptAt)
}

// WithSuccessfulCheck returns a new state with updated check times.
func (s ReleaseState) WithSuccessfulCheck(latest string, checkInterval time.Duration) ReleaseState {
	return ReleaseState{
		LastCheckedAt: time.Now(),
		NextAttemptAt: time.Now().Add(checkInterval),
		LatestVersion: latest,
		LastError:     "",
	}
}

// WithFailedCheck returns a new state with updated failure info and retry time.
func (s ReleaseState) WithFailedCheck(err error, retryAfterFailure time.Duration) ReleaseState {
	return ReleaseState{
		LastCheckedAt: s.LastCheckedAt,
		NextAttemptAt: time.Now().Add(retryAfterFailure),
		LatestVersion: s.LatestVersion,
		LastError:     err.Error(),
	}
}
