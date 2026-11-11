// Package tfstate reads and writes the OpenTofu state meshStack keeps for a building block, and
// serves it to tofu's http backend.
package tfstate

import (
	"context"
	"encoding/json/jsontext"
	"errors"
	"fmt"
	"net/url"

	"github.com/meshcloud/meshstack-cli/client/http"
	"github.com/meshcloud/meshstack-cli/client/types/xurl"
	"github.com/meshcloud/meshstack-cli/internal/json"
)

// ErrNoState is meshStack's 404, which tofu's http backend takes for a state not written yet.
var ErrNoState = errors.New("no state")

var ErrNoLock = errors.New("no lock")

type Store struct {
	BuildingBlock string
	Workspace     string

	client http.AuthorizedClient
	state  *url.URL
}

// NewStore must address the state as stateEndpointFormat in
// ../building-block-runner/internal/executor/tf/tfcmd.go does.
func NewStore(client http.AuthorizedClient, endpoint xurl.URL, workspace string, buildingBlock string) Store {
	return Store{
		BuildingBlock: buildingBlock, Workspace: workspace, client: client,
		state: endpoint.JoinPath("api", "terraform", "state", "workspace", workspace, "buildingBlock", buildingBlock),
	}
}

func (s Store) Get(ctx context.Context) ([]byte, error) {
	state, err := s.do(ctx, http.MethodGet, s.state)
	if httpErr, ok := errors.AsType[http.Error](err); ok && httpErr.IsNotFound() {
		return nil, ErrNoState
	}
	return state, s.wrap("read", err)
}

// Put and Delete send the ID of tofu's lock as tofu's http backend does, because meshStack refuses
// a write without the ID of the lock it holds.
func (s Store) Put(ctx context.Context, state []byte, lockId string) error {
	_, err := s.do(ctx, http.MethodPost, s.state, withLockId(lockId, jsonBody(state)...)...)
	return s.wrap("store", err)
}

func (s Store) Delete(ctx context.Context, lockId string) error {
	_, err := s.do(ctx, http.MethodDelete, s.state, withLockId(lockId)...)
	return s.wrap("delete", err)
}

func withLockId(lockId string, opts ...http.RequestOption) []http.RequestOption {
	if lockId == "" {
		return opts
	}
	return append(opts, http.WithUrlQuery(map[string]string{"ID": lockId}))
}

// Lock and Unlock take tofu's lock info as tofu sends it. A Lock that meshStack refuses with 423
// is an [http.Error] that carries the lock info of the holder.
func (s Store) Lock(ctx context.Context, info []byte) error {
	_, err := s.do(ctx, http.MethodPost, s.state.JoinPath("lock"), jsonBody(info)...)
	return s.wrap("lock", err)
}

// Unlock fails with a 423 of meshStack where meshStack holds a lock of another ID than the one in
// info.
func (s Store) Unlock(ctx context.Context, info []byte) error {
	_, err := s.do(ctx, http.MethodDelete, s.state.JoinPath("lock"), jsonBody(info)...)
	return s.wrap("unlock", err)
}

func (s Store) ReadLock(ctx context.Context) (lock Lock, err error) {
	read, err := s.do(ctx, http.MethodGet, s.state.JoinPath("lock"))
	if httpErr, ok := errors.AsType[http.Error](err); ok && httpErr.IsNotFound() {
		return lock, ErrNoLock
	} else if err != nil {
		return lock, s.wrap("read the lock on", err)
	}
	return lock, json.Unmarshal(read, &lock)
}

func (s Store) do(ctx context.Context, method string, target *url.URL, opts ...http.RequestOption) ([]byte, error) {
	if method != http.MethodGet {
		// A 423 goes back to tofu at once, which names the holder of the lock and waits itself.
		opts = append(opts, http.Retryable(http.RetryOnBusy))
	}
	return http.ReadAllBody(s.client.DoRequest(ctx, method, target, opts...))
}

func (s Store) wrap(action string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("cannot %s the state of building block %s in workspace %s: %w", action, s.BuildingBlock, s.Workspace, err)
}

// jsonBody sends no body for an empty one, as tofu's force-unlock sends it.
func jsonBody(body []byte) []http.RequestOption {
	if len(body) == 0 {
		return nil
	}
	return []http.RequestOption{http.WithJsonRequest(jsontext.Value(body), "")}
}
