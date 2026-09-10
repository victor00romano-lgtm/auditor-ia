package domain

import "time"

type Assignee struct {
	BitrixUserID int64
	DisplayName  string
	Active       *bool
	SyncedAt     time.Time
}
