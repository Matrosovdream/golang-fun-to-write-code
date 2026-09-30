package task

import "time"

type Status string

const (
	StatusOpen Status = "open"
	StatusDone Status = "done"
)

// Task is the domain model: no storage or presentation concerns belong here.
type Task struct {
	ID        int64      `json:"id"`
	Title     string     `json:"title"`
	Priority  int        `json:"priority"` // 1 = high, 2 = normal, 3 = low
	Status    Status     `json:"status"`
	CreatedAt time.Time  `json:"created_at"`
	DoneAt    *time.Time `json:"done_at,omitempty"` // nil until completed
}
