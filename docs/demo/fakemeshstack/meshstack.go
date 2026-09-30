package main

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json/v2"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

type installation struct {
	profile          string
	api, sso         string
	defaultWorkspace string
}

var installations = []installation{
	{profile: "production", api: "api.meshstack.example.com", sso: "sso.meshstack.example.com", defaultWorkspace: "platform-team"},
	{profile: "staging", api: "api.staging.meshstack.example.com", sso: "sso.staging.meshstack.example.com"},
	{profile: "dev", api: "api.dev.meshstack.example.com", sso: "sso.dev.meshstack.example.com"},
}

const (
	realm       = "/realms/meshfed"
	cliClientId = "meshstack-cli"
	// At least client.MinMeshStackVersion, or the CLI refuses the backend.
	meshStackVersion = "2026.39.0"
)

var workspaces = [][2]string{
	{"platform-team", "Platform Team"},
	{"customer-portal", "Customer Portal"},
	{"data-analytics", "Data Analytics"},
	{"payments", "Payments"},
	{"mobile-apps", "Mobile Apps"},
	{"machine-learning", "Machine Learning"},
	{"security-ops", "Security Operations"},
	{"identity-access", "Identity & Access"},
	{"marketing-web", "Marketing Websites"},
	{"internal-tools", "Internal Tools"},
	{"billing", "Billing Services"},
	{"search", "Search & Discovery"},
	{"logistics", "Logistics"},
	{"hr-systems", "HR Systems"},
	{"devex", "Developer Experience"},
	{"observability", "Observability"},
}

func (i installation) endpoint() string { return "https://" + i.api }
func (i installation) issuer() string   { return "https://" + i.sso + realm }

func (i installation) register(mux *http.ServeMux, idp *identityProvider) {
	mux.HandleFunc("GET "+i.api+"/mesh/info", func(w http.ResponseWriter, _ *http.Request) {
		writeJson(w, "application/json", http.StatusOK, map[string]any{
			"version":                  meshStackVersion,
			"issuer":                   i.issuer(),
			"cliClientId":              cliClientId,
			"adminWorkspaceIdentifier": "platform-team",
			"metadata":                 map[string]string{},
		})
	})
	mux.HandleFunc("GET "+i.api+"/api/meshobjects/meshworkspaces", listWorkspaces)
	mux.HandleFunc("GET "+i.api+"/api/meshobjects/meshbuildingblocks", listBuildingBlocks)

	oidc := i.sso + realm + "/protocol/openid-connect"
	mux.HandleFunc("GET "+i.sso+realm+"/.well-known/openid-configuration", func(w http.ResponseWriter, _ *http.Request) {
		writeJson(w, "application/json", http.StatusOK, map[string]any{
			"issuer":                 i.issuer(),
			"authorization_endpoint": "https://" + oidc + "/auth",
			"token_endpoint":         "https://" + oidc + "/token",
			"end_session_endpoint":   "https://" + oidc + "/logout",
		})
	})
	mux.HandleFunc("GET "+oidc+"/auth", idp.authorize)
	mux.HandleFunc("POST "+oidc+"/token", func(w http.ResponseWriter, r *http.Request) {
		idp.token(w, r, i.issuer())
	})
}

func listWorkspaces(w http.ResponseWriter, r *http.Request) {
	if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") {
		http.Error(w, "no bearer token", http.StatusUnauthorized)
		return
	}
	items := make([]map[string]any, 0, len(workspaces))
	for _, workspace := range workspaces {
		items = append(items, map[string]any{
			"metadata": map[string]any{"name": workspace[0], "createdOn": "2025-03-14T09:26:53Z", "tags": map[string]any{}},
			"spec":     map[string]any{"displayName": workspace[1]},
		})
	}
	writeJson(w, "application/vnd.meshcloud.api.meshworkspace.v2.hal+json", http.StatusOK, map[string]any{
		"_embedded": map[string]any{"meshWorkspaces": items},
		"page":      map[string]int{"size": len(items), "totalElements": len(items), "totalPages": 1, "number": 0},
	})
}

// buildingBlocks are more than a demo's --limit, so that the CLI says it listed only the first ones.
var buildingBlocks = [][3]string{
	{"PostgreSQL Database", "platform-team", "SUCCEEDED"},
	{"Azure Storage Account", "data-analytics", "SUCCEEDED"},
	{"GitHub Repository", "customer-portal", "SUCCEEDED"},
	{"Kubernetes Namespace", "payments", "IN_PROGRESS"},
	{"AWS S3 Bucket", "machine-learning", "SUCCEEDED"},
	{"Key Vault", "security-ops", "FAILED"},
	{"CI/CD Pipeline", "mobile-apps", "SUCCEEDED"},
	{"Redis Cache", "billing", "SUCCEEDED"},
	{"Budget Alert", "marketing-web", "SUCCEEDED"},
	{"Container Registry", "devex", "SUCCEEDED"},
	{"Log Analytics Workspace", "observability", "SUCCEEDED"},
	{"Service Principal", "identity-access", "SUCCEEDED"},
	{"PostgreSQL Database", "logistics", "SUCCEEDED"},
	{"GitHub Repository", "internal-tools", "SUCCEEDED"},
	{"Kubernetes Namespace", "search", "SUCCEEDED"},
	{"DNS Zone", "customer-portal", "SUCCEEDED"},
	{"AWS S3 Bucket", "hr-systems", "WAITING_FOR_OPERATOR_INPUT"},
	{"Budget Alert", "payments", "SUCCEEDED"},
	{"Key Vault", "platform-team", "SUCCEEDED"},
	{"CI/CD Pipeline", "data-analytics", "SUCCEEDED"},
	{"Redis Cache", "customer-portal", "SUCCEEDED"},
	{"Container Registry", "platform-team", "SUCCEEDED"},
	{"DNS Zone", "marketing-web", "SUCCEEDED"},
}

func listBuildingBlocks(w http.ResponseWriter, r *http.Request) {
	if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") {
		http.Error(w, "no bearer token", http.StatusUnauthorized)
		return
	}
	size, err := strconv.Atoi(r.URL.Query().Get("size"))
	if err != nil || size < 1 {
		size = 20
	}
	number, _ := strconv.Atoi(r.URL.Query().Get("page"))
	start := min(number*size, len(buildingBlocks))
	type buildingBlock struct {
		Metadata struct {
			Uuid             string `json:"uuid"`
			OwnedByWorkspace string `json:"ownedByWorkspace"`
		} `json:"metadata"`
		Spec struct {
			DisplayName string `json:"displayName"`
		} `json:"spec"`
		Status struct {
			Status string `json:"status"`
		} `json:"status"`
	}
	items := make([]buildingBlock, 0, size)
	for _, block := range buildingBlocks[start:min(start+size, len(buildingBlocks))] {
		var item buildingBlock
		item.Metadata.Uuid, item.Metadata.OwnedByWorkspace = stableUuid(block[0]+block[1]), block[1]
		item.Spec.DisplayName, item.Status.Status = block[0], block[2]
		items = append(items, item)
	}
	writeJson(w, "application/vnd.meshcloud.api.meshBuildingBlock.v2-preview.hal+json", http.StatusOK, map[string]any{
		"_embedded": map[string]any{"meshBuildingBlocks": items},
		"page": map[string]int{
			"size": size, "totalElements": len(buildingBlocks), "totalPages": (len(buildingBlocks) + size - 1) / size, "number": number,
		},
	})
}

// stableUuid keeps the recorded output the same from one recording to the next.
func stableUuid(of string) string {
	sum := sha256.Sum256([]byte(of))
	return fmt.Sprintf("%x-%x-%x-%x-%x", sum[0:4], sum[4:6], sum[6:8], sum[8:10], sum[10:16])
}

type identityProvider struct {
	mu    sync.Mutex
	codes map[string]authorization
}

type authorization struct {
	challenge, redirectURI, scope string
}

func newIdentityProvider() *identityProvider {
	return &identityProvider{codes: map[string]authorization{}}
}

// authorize logs the person in at once, as a browser with a live Keycloak session would.
func (idp *identityProvider) authorize(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	redirect, err := url.Parse(query.Get("redirect_uri"))
	if err != nil || query.Get("client_id") != cliClientId || query.Get("code_challenge_method") != "S256" {
		http.Error(w, "not an authorization request of the meshStack CLI", http.StatusBadRequest)
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
	http.Redirect(w, r, redirect.String(), http.StatusFound)
}

func (idp *identityProvider) token(w http.ResponseWriter, r *http.Request, issuer string) {
	scope := r.PostFormValue("scope")
	switch r.PostFormValue("grant_type") {
	case "authorization_code":
		idp.mu.Lock()
		granted, found := idp.codes[r.PostFormValue("code")]
		delete(idp.codes, r.PostFormValue("code"))
		idp.mu.Unlock()
		verifier := sha256.Sum256([]byte(r.PostFormValue("code_verifier")))
		if !found || granted.redirectURI != r.PostFormValue("redirect_uri") ||
			base64.RawURLEncoding.EncodeToString(verifier[:]) != granted.challenge {
			tokenError(w, "invalid_grant", "the code, its redirect_uri or its code_verifier does not match")
			return
		}
		scope = granted.scope
	case "refresh_token":
		if r.PostFormValue("refresh_token") == "" {
			tokenError(w, "invalid_grant", "no refresh token")
			return
		}
	default:
		tokenError(w, "unsupported_grant_type", r.PostFormValue("grant_type"))
		return
	}
	writeJson(w, "application/json", http.StatusOK, map[string]any{
		"access_token":  accessToken(issuer, scope),
		"token_type":    "Bearer",
		"expires_in":    300,
		"refresh_token": rand.Text(),
		"scope":         scope,
	})
}

// accessToken is unsigned: the CLI reads its claims and leaves verifying it to meshStack. A c:
// scope becomes the MC_CUSTOMER claim, as Keycloak's mapper writes it, see internal/oidc/jwt.
func accessToken(issuer, scope string) string {
	claims := map[string]any{
		"iss":                issuer,
		"sub":                "3f1c2a9e-5b7d-4e8a-9c61-2d0b7f4e8a13",
		"azp":                cliClientId,
		"preferred_username": "jane.doe@example.com",
		"exp":                time.Now().Add(5 * time.Minute).Unix(),
		"scope":              scope,
	}
	for s := range strings.FieldsSeq(scope) {
		if workspace, found := strings.CutPrefix(s, "c:"); found {
			claims["MC_CUSTOMER"] = workspace
		}
	}
	encode := func(v any) string {
		content, _ := json.Marshal(v)
		return base64.RawURLEncoding.EncodeToString(content)
	}
	return encode(map[string]string{"alg": "none", "typ": "JWT"}) + "." + encode(claims) + "."
}

func tokenError(w http.ResponseWriter, code, description string) {
	writeJson(w, "application/json", http.StatusBadRequest, map[string]string{"error": code, "error_description": description})
}

func writeJson(w http.ResponseWriter, contentType string, status int, body any) {
	w.Header().Set("Content-Type", contentType)
	w.WriteHeader(status)
	// Sorted, as meshStack writes _embedded before page.
	_ = json.MarshalWrite(w, body, json.Deterministic(true))
}
