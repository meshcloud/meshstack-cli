package tfstate

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/meshcloud/meshstack-cli/client"
	"github.com/meshcloud/meshstack-cli/client/http"
	"github.com/meshcloud/meshstack-cli/internal/json"
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

// NoUnfinishedRun passes a lock that a person holds.
func (l Lock) NoUnfinishedRun(ctx context.Context, runs client.MeshBuildingBlockRunClient) error {
	if l.Holder.RunUuid == "" {
		return nil
	}
	runUuid := l.Holder.RunUuid
	run, err := runs.Read(ctx, runUuid)
	switch {
	case err != nil:
		return err
	case run == nil:
		return fmt.Errorf("building block run %s holds the lock, but meshStack does not know it", runUuid)
	case run.IsRunning():
		return fmt.Errorf("building block run %s holds the lock, is %s and is still writing the state", runUuid, run.Status)
	}
	return nil
}

var ErrLockReplaced = errors.New("the lock was already released, and the state now has a different lock")

// Release releases lock by the ID it was read with, so that a lock taken since stays.
func (s Store) Release(ctx context.Context, lock Lock) error {
	info, err := json.Marshal(lock.Info)
	if err != nil {
		return err
	}
	err = s.Unlock(ctx, info)
	if httpErr, ok := errors.AsType[http.Error](err); ok && httpErr.IsLocked() {
		return fmt.Errorf("%w\n\n%w", err, ErrLockReplaced)
	}
	return err
}
