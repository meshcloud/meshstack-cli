package jwt

import (
	_ "embed"
	"encoding/json/v2"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/meshcloud/meshstack-cli/internal/meshstack"
	"github.com/meshcloud/meshstack-cli/internal/testutil/jsontest"
)

var (
	//go:embed testdata/jwt_unscoped.json
	unscopedToken []byte
	//go:embed testdata/jwt_unscoped_no_exp.json
	unscopedTokenNoExp []byte
	//go:embed testdata/jwt_scoped.json
	scopedToken []byte
	//go:embed testdata/jwt_opaque.json
	opaqueToken []byte
	//go:embed testdata/jwt_not_base64.json
	notBase64Token []byte
	//go:embed testdata/jwt_not_json.json
	notJsonToken []byte
)

func TestJWT(t *testing.T) {
	t.Run("an unscoped token", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			token := jsontest.MustUnmarshal[JWT](t, unscopedToken)
			expiry := ExpiryClaim.getFrom(token)
			assert.Equal(t, Expiry{time.Unix(1767225600, 0)}, expiry)
			assert.False(t, expiry.Expired(0))
			assert.True(t, expiry.Expired(time.Until(expiry.Time)))
			synctest.Sleep(1000000 * time.Hour) // luckily, we're in a synctest bubble...
			assert.True(t, expiry.Expired(30*time.Second))
		})
	})
	t.Run("an unscoped token without exp", func(t *testing.T) {
		token := jsontest.MustUnmarshal[JWT](t, unscopedTokenNoExp)
		expiry := ExpiryClaim.getFrom(token)
		assert.Equal(t, Expiry{}, expiry)
		assert.True(t, expiry.Expired(0))
	})
	t.Run("a token scoped to a workspace", func(t *testing.T) {
		token := jsontest.MustUnmarshal[JWT](t, scopedToken)
		// Verified against a live keycloak: the claim carries the identifier, not the c: scope the
		// token was asked for.
		assert.Equal(t, meshstack.Workspace("demo-partner"), WorkspaceClaim.getFrom(token))
	})
	t.Run("an unscoped token names no workspace", func(t *testing.T) {
		token := jsontest.MustUnmarshal[JWT](t, unscopedToken)
		assert.Equal(t, meshstack.NoWorkspace, WorkspaceClaim.getFrom(token))
	})
}

func TestJWTBroken(t *testing.T) {
	for _, test := range []struct {
		name    string
		payload []byte
		wantErr assert.ErrorAssertionFunc
	}{
		{"an opaque token", opaqueToken, func(t assert.TestingT, err error, args ...any) bool {
			return assert.ErrorContains(t, err, "not a JWT", args...)
		}},
		{"a payload that is not base64", notBase64Token, func(t assert.TestingT, err error, args ...any) bool {
			return assert.ErrorContains(t, err, "cannot parse JWT payload as JSON", args...)
		}},
		{"a payload that is not JSON", notJsonToken, func(t assert.TestingT, err error, args ...any) bool {
			return assert.ErrorContains(t, err, "cannot parse JWT payload as JSON", args...)
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			var token JWT
			test.wantErr(t, json.Unmarshal(test.payload, &token))
		})
	}
}
