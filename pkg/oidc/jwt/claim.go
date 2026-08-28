package jwt

import (
	"time"

	"github.com/meshcloud/meshstack-cli/pkg/workspace"
)

type Claim[V any] struct {
	key       string
	converter func(v any) V
}

var (
	WorkspaceClaim = Claim[workspace.Name]{
		key: workspace.ClaimKey,
		converter: func(v any) workspace.Name {
			name, _ := v.(string)
			return workspace.Name(name)
		},
	}
	// A token that says nothing about its own life reads as nil, not as 1970: nothing can
	// renew one, so the server is what decides when it stops working.
	Expiry = Claim[*time.Time]{
		key: "exp",
		converter: func(v any) *time.Time {
			// JSON numbers decode as float64, and exp counts seconds since the epoch.
			seconds, ok := v.(float64)
			if !ok {
				return nil
			}
			expiry := time.Unix(int64(seconds), 0)
			return &expiry
		},
	}
	UsernameClaim = Claim[string]{key: "preferred_username"}
)

func (c Claim[V]) GetFrom(jwt JWT) V {
	if c.converter != nil {
		return c.converter(jwt.claims[c.key])
	}
	value, _ := jwt.claims[c.key].(V)
	return value
}
