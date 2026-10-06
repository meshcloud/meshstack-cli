package tfstate

import (
	"fmt"
	"time"
)

type Lock struct {
	Info      LockInfo   `json:"lockInfo"`
	Holder    LockHolder `json:"holder"`
	CreatedOn time.Time  `json:"createdOn"`
}

type LockHolder struct {
	// RunUuid is empty for a lock that a person holds.
	RunUuid   string `json:"runUuid"`
	Principal string `json:"principal"`
}

// LockInfo is the part of tofu's lock info that a person needs to judge a lock. The field names are
// tofu's, which meshStack stores as tofu sent them.
type LockInfo struct {
	ID        string `json:"ID"`
	Operation string `json:"Operation"`
	Who       string `json:"Who"`
}

func (l Lock) String() string {
	holder := l.Holder.Principal
	if l.Holder.RunUuid != "" {
		holder = "building block run " + l.Holder.RunUuid
	}
	return fmt.Sprintf("%s since %s, for tofu %s (lock ID %s)", holder, l.CreatedOn.Format(time.RFC3339), l.Info.Operation, l.Info.ID)
}
