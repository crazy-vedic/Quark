package domain

import "time"

const (
	ScheduledRunPending   = "pending"
	ScheduledRunRunning   = "running"
	ScheduledRunCompleted = "completed"
	ScheduledRunFailed    = "failed"
	ScheduledRunCancelled = "cancelled"
)

// ScheduledRun is a persisted delayed execution for a saved request.
type ScheduledRun struct {
	ID        string
	RequestID string
	RunAt     time.Time
	Status    string
	LastError string
	CreatedAt time.Time
	UpdatedAt time.Time
}
