// Package tfstate reads and writes the OpenTofu state meshStack keeps for a building block, and
// serves it to tofu's http backend.
package tfstate

import (
	"context"
	"errors"
	"fmt"
	gohttp "net/http"
	"uuid"

	"github.com/meshcloud/meshstack-cli/internal/http"
)

// ErrNoState is meshStack's 404, which tofu's http backend takes for a state not written yet.
var ErrNoState = errors.New("no state")

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

func (s Store) Put(ctx context.Context, state []byte) error {
	_, err := s.raw.DoRequest(ctx, http.MethodPost, s.path(), http.WithBody(state),
		http.WithHeaders(gohttp.Header{"Content-Type": {"application/json"}}))
	return s.wrap("store", err)
}

func (s Store) Delete(ctx context.Context) error {
	_, err := s.raw.DoRequest(ctx, http.MethodDelete, s.path())
	return s.wrap("delete", err)
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
