// Package oidc speaks the OIDC protocol to a meshStack instance's identity provider, and
// nothing else.
//
// It returns plain strings and a duration rather than a profile.IssuedToken, so it never
// imports pkg/profile — otherwise pkg/profile could not own the on-disk types without an
// import cycle. It never touches the filesystem either: pkg/auth calls it while holding the
// store's lock, which is what makes the rotated refresh token and the new access token one
// atomic write.
//
// The browser half of the protocol lives in pkg/oidc/browser, which the Terraform provider
// must never reach; .golangci.yml holds that boundary.
package oidc

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/meshcloud/meshstack-cli/pkg/workspace"
)

// ClientConfig is everything needed to talk to the meshStack instance's identity provider.
type ClientConfig struct {
	Endpoint              *url.URL // the meshStack API endpoint the discovery started from
	Issuer                string
	ClientId              string // the public CLI client from /mesh/info
	AuthorizationEndpoint string
	TokenEndpoint         string
	EndSessionEndpoint    string
	RevocationEndpoint    string
}

// requestTimeout bounds every exchange on top of the caller's context: an identity provider
// that accepts the connection and then goes quiet must not hang a command forever.
const requestTimeout = 30 * time.Second

const userAgent = "meshstack-cli"

// httpClient is shared so that discovery and the grant that follows it reuse one connection.
// It carries no cookie jar and no redirect policy of its own, because every endpoint here
// answers in one hop.
var httpClient = &http.Client{Timeout: requestTimeout}

// maxBodyBytes caps what an error path reads back. Nothing this package parses is large, and
// a misrouted request can land on a page that is.
const maxBodyBytes = 1 << 20

// claimWorkspace is written by meshfed's MC_CUSTOMER.js script mapper from the c:<workspace>
// scope on the token request. The name is c for customer, from before workspaces were called
// workspaces.
const claimWorkspace = "MC_CUSTOMER"

// Claims decodes a token payload without verifying its signature, for MC_CUSTOMER and exp.
// Nothing is authorized on the strength of this, so no verification is needed or implied.
func Claims(accessToken string) (map[string]any, error) {
	parts := strings.Split(accessToken, ".")
	if len(parts) != 3 {
		return nil, fmt.Errorf("the token is not a JWT: it has %d dot-separated parts rather than 3", len(parts))
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, fmt.Errorf("cannot decode the token payload: %w", err)
	}
	var claims map[string]any
	if err := json.Unmarshal(payload, &claims); err != nil {
		return nil, fmt.Errorf("cannot parse the token payload as JSON: %w", err)
	}
	return claims, nil
}

// ClaimWorkspace returns the workspace the token carries, from the MC_CUSTOMER claim.
// A token for a workspace the user is not a member of comes back HTTP 200 with the claim
// absent, so this is what turns that into an error message rather than a later 403.
func ClaimWorkspace(accessToken string) workspace.Name {
	claims, err := Claims(accessToken)
	if err != nil {
		return ""
	}
	name, _ := claims[claimWorkspace].(string)
	return workspace.Name(name)
}

// Expiry returns the token's exp claim, and false when it carries none. A refresh token is
// one that carries none — it is typ Offline — so never call this to find out when a login
// dies.
func Expiry(accessToken string) (time.Time, bool) {
	claims, err := Claims(accessToken)
	if err != nil {
		return time.Time{}, false
	}
	// JSON numbers decode as float64, and exp counts seconds since the epoch.
	exp, ok := claims["exp"].(float64)
	if !ok {
		return time.Time{}, false
	}
	return time.Unix(int64(exp), 0), true
}

// getJSON reads one JSON document. Both documents it fetches — /mesh/info and the OIDC
// configuration — are public and unauthenticated, so no request here carries a credential.
func getJSON[T any](ctx context.Context, target string) (T, error) {
	var out T

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return out, fmt.Errorf("cannot build a request for %s: %w", target, err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", userAgent)

	resp, err := httpClient.Do(req)
	if err != nil {
		return out, fmt.Errorf("cannot reach %s: %w", target, err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
	if err != nil {
		return out, fmt.Errorf("cannot read the response from %s: %w", target, err)
	}
	if resp.StatusCode != http.StatusOK {
		return out, fmt.Errorf("%s returned HTTP %d: %s", target, resp.StatusCode, excerpt(body))
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return out, fmt.Errorf("cannot parse the response from %s as JSON: %w", target, err)
	}
	return out, nil
}

// excerpt makes a response body fit in one line of an error message.
func excerpt(body []byte) string {
	line := strings.Join(strings.Fields(string(body)), " ")
	if line == "" {
		return "(empty body)"
	}
	if len(line) > 200 {
		return line[:200] + "..."
	}
	return line
}
