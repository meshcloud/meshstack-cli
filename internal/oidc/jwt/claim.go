package jwt

import (
	"time"
)

type Claim[V any] struct {
	key       string
	converter func(v any) V
}

var (
	ExpiryClaim = Claim[Expiry]{
		key: "exp",
		converter: func(v any) (expiry Expiry) {
			// JSON numbers decode as float64, and exp counts seconds since the epoch.
			seconds, ok := v.(float64)
			if !ok {
				return
			}
			expiry.Time = time.Unix(int64(seconds), 0)
			return
		},
	}
)

func (c Claim[V]) getFrom(jwt JWT) V {
	if c.converter != nil {
		return c.converter(jwt.claims[c.key])
	}
	value, _ := jwt.claims[c.key].(V)
	return value
}

type Expiry struct {
	time.Time
}

func (expiry Expiry) Expired(grace time.Duration) bool {
	if expiry == (Expiry{}) {
		// JWT tokens not having an exp are considered expired (default value returned in converter above)
		return true
	}
	return time.Until(expiry.Time) <= grace
}
