package tfstate

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"time"
	"uuid"

	"github.com/meshcloud/meshstack-cli/client"
	"github.com/meshcloud/meshstack-cli/internal/http"
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
func (l Lock) NoUnfinishedRun(ctx context.Context, raw *client.RawClient) error {
	if l.Holder.RunUuid == "" {
		return nil
	}
	runUuid, err := uuid.Parse(l.Holder.RunUuid)
	if err != nil {
		return fmt.Errorf("the lock names the run %q, which is no uuid: %w", l.Holder.RunUuid, err)
	}
	read, err := raw.Get[client.MeshBuildingBlockRun](ctx, runUuid)
	if err != nil {
		return err
	}
	var run client.MeshBuildingBlockRun
	if err = json.Unmarshal(read, &run); err != nil {
		return err
	}
	if run.Status == string(client.BuildingBlockStatusPending) || run.Status == string(client.BuildingBlockStatusInProgress) {
		return fmt.Errorf("building block run %s, which holds the lock, is %s and still writes the state", runUuid, run.Status)
	}
	return nil
}

var ErrLockReplaced = errors.New("the state has another lock now, so the lock to release is released already")

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
