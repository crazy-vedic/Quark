package schedule_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/crazy-vedic/quark/internal/schedule"
)

func TestParseWhen_DurationAndInPrefix(t *testing.T) {
	now := time.Date(2026, 6, 25, 15, 0, 0, 0, time.UTC)

	got, err := schedule.ParseWhen("in 10m", now)
	require.NoError(t, err)
	assert.Equal(t, now.Add(10*time.Minute), got)

	got, err = schedule.ParseWhen("1h30m", now)
	require.NoError(t, err)
	assert.Equal(t, now.Add(90*time.Minute), got)
}

func TestParseWhen_ClockTimeRollsToTomorrowWhenPassed(t *testing.T) {
	loc := time.FixedZone("IST", 19800)
	now := time.Date(2026, 6, 25, 15, 0, 0, 0, loc)

	got, err := schedule.ParseWhen("14:30", now)
	require.NoError(t, err)
	assert.Equal(t, time.Date(2026, 6, 26, 14, 30, 0, 0, loc), got)
}

func TestParseWhen_RejectsEmptyAndNonPositiveDelay(t *testing.T) {
	now := time.Date(2026, 6, 25, 15, 0, 0, 0, time.UTC)

	_, err := schedule.ParseWhen("", now)
	require.Error(t, err)

	_, err = schedule.ParseWhen("-1m", now)
	require.Error(t, err)
}
