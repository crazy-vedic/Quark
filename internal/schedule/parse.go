package schedule

import (
	"fmt"
	"strings"
	"time"
)

var absoluteLayouts = []string{
	time.RFC3339,
	"2006-01-02 15:04",
	"2006-01-02 15:04:05",
	"2006-01-02",
	"15:04",
}

// ParseWhen parses a delayed or absolute run time.
//
// Supported examples:
//   - 10m, 1h30m
//   - in 10m
//   - RFC3339
//   - 2006-01-02 15:04
//   - 15:04, resolved to today or tomorrow if already passed
func ParseWhen(raw string, now time.Time) (time.Time, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return time.Time{}, fmt.Errorf("schedule: time cannot be empty")
	}
	value = strings.TrimPrefix(strings.ToLower(value), "in ")
	if d, err := time.ParseDuration(value); err == nil {
		if d <= 0 {
			return time.Time{}, fmt.Errorf("schedule: delay must be positive")
		}
		return now.Add(d), nil
	}

	for _, layout := range absoluteLayouts {
		parsed, err := time.ParseInLocation(layout, raw, now.Location())
		if err != nil {
			continue
		}
		if layout == "15:04" {
			parsed = time.Date(
				now.Year(), now.Month(), now.Day(),
				parsed.Hour(), parsed.Minute(), 0, 0, now.Location(),
			)
			if !parsed.After(now) {
				parsed = parsed.Add(24 * time.Hour)
			}
		}
		if layout == "2006-01-02" {
			parsed = time.Date(
				parsed.Year(),
				parsed.Month(),
				parsed.Day(),
				0,
				0,
				0,
				0,
				now.Location(),
			)
		}
		return parsed, nil
	}

	return time.Time{}, fmt.Errorf("schedule: unsupported time %q", raw)
}
