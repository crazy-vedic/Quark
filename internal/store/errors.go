package store

import "errors"

// Sentinel errors for classification at call sites via errors.Is.
var (
	ErrNotFound  = errors.New("not found")
	ErrDuplicate = errors.New("duplicate name")
)
