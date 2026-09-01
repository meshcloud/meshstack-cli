package jwt

import (
	_ "embed"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// All fabricated: only the payload is ever real, because nothing here reads the header or
// verifies the signature.
var (
	//go:embed testdata/jwt_workspace
	workspaceToken string
	//go:embed testdata/jwt_unscoped
	unscopedToken string
	//go:embed testdata/jwt_opaque
	opaqueToken string
	//go:embed testdata/jwt_not_base64
	notBase64Token string
	//go:embed testdata/jwt_not_json
	notJsonToken string
)

func TestClaims(t *testing.T) {
	expiry := time.Unix(1767225600, 0)

	t.Run("a token scoped to a workspace", func(t *testing.T) {
		token := parse(t, workspaceToken)
		assert.Equal(t, "demo", WorkspaceClaim.GetFrom(token))
		assert.Equal(t, &expiry, Expiry.GetFrom(token))
	})

	t.Run("an unscoped token", func(t *testing.T) {
		token := parse(t, unscopedToken)
		assert.Empty(t, WorkspaceClaim.GetFrom(token), "an unscoped token carries no MC_CUSTOMER")
		assert.Equal(t, &expiry, Expiry.GetFrom(token))
	})
}

// TestRefusedTokens holds the rule that every meshStack access token is a JWT, including one
// a person pasted: a text that cannot be read as one is refused where it arrives, rather than
// stored and then found unreadable by a later command.
func TestRefusedTokens(t *testing.T) {
	for _, test := range []struct {
		name  string
		text  string
		wants string
	}{
		{"an opaque token", opaqueToken, "not a JWT"},
		{"a payload that is not base64", notBase64Token, "cannot decode the JWT payload"},
		{"a payload that is not JSON", notJsonToken, "cannot parse JWT payload as JSON"},
	} {
		t.Run(test.name, func(t *testing.T) {
			token, err := Parse(strings.TrimSpace(test.text))
			require.ErrorContains(t, err, test.wants)
			assert.Equal(t, strings.TrimSpace(test.text), token.String,
				"the text is kept even when it could not be read, so an error can quote it")
		})
	}
}

func parse(t *testing.T, text string) JWT {
	t.Helper()
	var token JWT
	require.NoError(t, token.UnmarshalText([]byte(strings.TrimSpace(text))))
	return token
}
