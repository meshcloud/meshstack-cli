package fakemeshstack

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json/v2"
	gohttp "net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const cliClientId = "meshstack-cli"

type identityProvider struct {
	mu    sync.Mutex
	codes map[string]authorization
}

type authorization struct {
	challenge, redirectUri, scope string
}

func (s *Server) registerKeycloak() {
	issuer, err := url.Parse(s.options.Issuer)
	if err != nil {
		panic(err)
	}
	openIdConnect := s.options.Issuer + "/protocol/openid-connect"
	s.routes.HandleFunc("GET "+issuer.Path+"/.well-known/openid-configuration", func(w gohttp.ResponseWriter, _ *gohttp.Request) {
		writeJson(w, gohttp.StatusOK, map[string]any{
			"issuer":                 s.options.Issuer,
			"authorization_endpoint": openIdConnect + "/auth",
			"token_endpoint":         openIdConnect + "/token",
			"end_session_endpoint":   openIdConnect + "/logout",
		})
	})
	s.routes.HandleFunc("GET "+issuer.Path+"/protocol/openid-connect/auth", s.identity.authorize)
	s.routes.HandleFunc("POST "+issuer.Path+"/protocol/openid-connect/token", s.token)
}

// authorize logs the person in at once, as a browser with a live Keycloak session would.
func (idp *identityProvider) authorize(w gohttp.ResponseWriter, r *gohttp.Request) {
	query := r.URL.Query()
	redirect, err := url.Parse(query.Get("redirect_uri"))
	if err != nil || query.Get("client_id") != cliClientId || query.Get("code_challenge_method") != "S256" {
		gohttp.Error(w, "not an authorization request of the meshStack CLI", gohttp.StatusBadRequest)
		return
	}
	code := rand.Text()
	idp.mu.Lock()
	idp.codes[code] = authorization{query.Get("code_challenge"), redirect.String(), query.Get("scope")}
	idp.mu.Unlock()

	callback := redirect.Query()
	callback.Set("code", code)
	callback.Set("state", query.Get("state"))
	redirect.RawQuery = callback.Encode()
	gohttp.Redirect(w, r, redirect.String(), gohttp.StatusFound) //nolint:gosec // G710: the redirect goes back to the CLI that asked, as Keycloak's does
}

func (idp *identityProvider) redeem(r *gohttp.Request) (scope string, redeemed bool) {
	idp.mu.Lock()
	granted, found := idp.codes[r.PostFormValue("code")]
	delete(idp.codes, r.PostFormValue("code"))
	idp.mu.Unlock()
	verifier := sha256.Sum256([]byte(r.PostFormValue("code_verifier")))
	return granted.scope, found && granted.redirectUri == r.PostFormValue("redirect_uri") &&
		base64Url(verifier[:]) == granted.challenge
}

func (s *Server) token(w gohttp.ResponseWriter, r *gohttp.Request) {
	scope := r.PostFormValue("scope")
	switch r.PostFormValue("grant_type") {
	case "authorization_code":
		granted, redeemed := s.identity.redeem(r)
		if !redeemed {
			tokenError(w, "invalid_grant", "the code, its redirect_uri or its code_verifier does not match")
			return
		}
		scope = granted
	case "refresh_token":
		if r.PostFormValue("refresh_token") == "" {
			tokenError(w, "invalid_grant", "no refresh token")
			return
		}
	default:
		tokenError(w, "unsupported_grant_type", r.PostFormValue("grant_type"))
		return
	}
	token := s.accessToken(scope)
	s.mu.Lock()
	s.honor(token)
	s.mu.Unlock()
	writeJson(w, gohttp.StatusOK, map[string]any{
		"access_token":  token,
		"token_type":    "Bearer",
		"expires_in":    300,
		"refresh_token": rand.Text(),
		// Keycloak's session lifetime for the meshstack-cli client, see
		// ../meshfed-release/ci/keycloak/container/realms.json.
		"refresh_expires_in": 86400,
		"scope":              scope,
	})
}

// accessToken is unsigned: the CLI reads its claims and leaves verifying it to meshStack. A c:
// scope becomes the MC_CUSTOMER claim, as Keycloak's mapper writes it, see internal/oidc/jwt.
func (s *Server) accessToken(scope string) string {
	claims := map[string]any{
		"iss":                s.options.Issuer,
		"sub":                "3f1c2a9e-5b7d-4e8a-9c61-2d0b7f4e8a13",
		"azp":                cliClientId,
		"preferred_username": "jane.doe@example.com",
		"exp":                time.Now().Add(5 * time.Minute).Unix(),
		"scope":              scope,
	}
	for field := range strings.FieldsSeq(scope) {
		if workspace, found := strings.CutPrefix(field, "c:"); found {
			claims["MC_CUSTOMER"] = workspace
		}
	}
	header, _ := json.Marshal(map[string]string{"alg": "none", "typ": "JWT"})
	payload, _ := json.Marshal(claims)
	return base64Url(header) + "." + base64Url(payload) + "."
}

func tokenError(w gohttp.ResponseWriter, code, description string) {
	writeJson(w, gohttp.StatusBadRequest, map[string]string{"error": code, "error_description": description})
}

func base64Url(content []byte) string {
	return base64.RawURLEncoding.EncodeToString(content)
}
