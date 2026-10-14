// Package tfstate reads and writes the OpenTofu state meshStack keeps for a building block, and
// serves it to tofu's http backend.
package tfstate

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	gohttp "net/http"
	"uuid"

	"github.com/meshcloud/meshstack-cli/internal/http"
)

// ErrNoState is meshStack's 404, which tofu's http backend takes for a state not written yet.
var ErrNoState = errors.New("no state")

var ErrNoLock = errors.New("no lock")

// Requester is implemented by client.RawClient.
type Requester interface {
	DoRequest(ctx context.Context, method, path string, opts ...http.RequestOption) ([]byte, error)
}

type Store struct {
	BuildingBlock uuid.UUID
	Workspace     string

	raw Requester
}

func NewStore(raw Requester, workspace string, buildingBlock uuid.UUID) Store {
	return Store{BuildingBlock: buildingBlock, Workspace: workspace, raw: raw}
}

func (s Store) Get(ctx context.Context) ([]byte, error) {
	state, err := s.raw.DoRequest(ctx, http.MethodGet, s.path())
	if httpErr, ok := errors.AsType[http.Error](err); ok && httpErr.IsNotFound() {
		return nil, ErrNoState
	}
	return state, s.wrap("read", err)
}

// Put and Delete send the ID of tofu's lock as tofu's http backend does, because meshStack refuses
// a write without the ID of the lock it holds.
func (s Store) Put(ctx context.Context, state []byte, lockId string) error {
	_, err := s.raw.DoRequest(ctx, http.MethodPost, s.path(), withLockId(lockId, http.WithBody(state), jsonContent)...)
	return s.wrap("store", err)
}

func (s Store) Delete(ctx context.Context, lockId string) error {
	_, err := s.raw.DoRequest(ctx, http.MethodDelete, s.path(), withLockId(lockId)...)
	return s.wrap("delete", err)
}

func withLockId(lockId string, opts ...http.RequestOption) []http.RequestOption {
	if lockId == "" {
		return opts
	}
	return append(opts, http.WithUrlQuery(map[string]string{"ID": lockId}))
}

var jsonContent = http.WithHeaders(gohttp.Header{"Content-Type": {"application/json"}})

// Lock and Unlock take tofu's lock info as tofu sends it. A Lock that meshStack refuses with 423
// is an [http.Error] that carries the lock info of the holder.
func (s Store) Lock(ctx context.Context, info []byte) error {
	_, err := s.raw.DoRequest(ctx, http.MethodPost, s.lockPath(), http.WithBody(info), jsonContent)
	return s.wrap("lock", err)
}

// Unlock fails with a 423 of meshStack where meshStack holds a lock of another ID than the one in
// info.
func (s Store) Unlock(ctx context.Context, info []byte) error {
	_, err := s.raw.DoRequest(ctx, http.MethodDelete, s.lockPath(), http.WithBody(info), jsonContent)
	return s.wrap("unlock", err)
}

func (s Store) ReadLock(ctx context.Context) (lock Lock, err error) {
	read, err := s.raw.DoRequest(ctx, http.MethodGet, s.lockPath())
	if httpErr, ok := errors.AsType[http.Error](err); ok && httpErr.IsNotFound() {
		return lock, ErrNoLock
	} else if err != nil {
		return lock, s.wrap("read the lock on", err)
	}
	return lock, json.Unmarshal(read, &lock)
}

func (s Store) wrap(action string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("cannot %s the state of building block %s in workspace %s: %w", action, s.BuildingBlock, s.Workspace, err)
}

// path must match EP_State in ../building-block-runner/tf-block-runner/tfrun/runapi.go, the address
// that the runner's backend uses. meshfed's TfHttpStateController serves it.
func (s Store) path() string {
	return fmt.Sprintf("/api/terraform/state/workspace/%s/buildingBlock/%s", s.Workspace, s.BuildingBlock)
}

func (s Store) lockPath() string {
	return s.path() + "/lock"
}
