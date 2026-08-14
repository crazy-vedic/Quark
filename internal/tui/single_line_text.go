package tui

import (
	"fmt"
	"unicode/utf8"
)

const singleLineLargeInputThreshold = 256

func singleLineInputWarning(value string) string {
	count := utf8.RuneCountInString(value)
	if count <= singleLineLargeInputThreshold {
		return ""
	}
	return warnStyle.Render(fmt.Sprintf(
		"  Large single-line input (%d characters). Consider an environment variable and reference it here.", count,
	))
}
