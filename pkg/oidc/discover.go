package oidc

import (
	"context"
	"fmt"
	"net/url"
	"strings"
)

// meshInfo is the part of the public /mesh/info document this package needs. The endpoint is
// unauthenticated, which is what lets discovery run before any credential exists.
type meshInfo struct {
	Version     string `json:"version"`
	Issuer      string `json:"issuer"`
	CliClientId string `json:"cliClientId"`
}

// providerMetadata is the part of the issuer's OIDC configuration this package uses.
type providerMetadata struct {
	Issuer                string `json:"issuer"`
	AuthorizationEndpoint string `json:"authorization_endpoint"`
	TokenEndpoint         string `json:"token_endpoint"`
	EndSessionEndpoint    string `json:"end_session_endpoint"`
	RevocationEndpoint    string `json:"revocation_endpoint"`
}

// Discover reads /mesh/info and then the issuer's OIDC configuration.
func Discover(ctx context.Context, endpoint *url.URL) (ClientConfig, error) {
	if endpoint == nil {
		return ClientConfig{}, fmt.Errorf("cannot discover the identity provider without a meshStack endpoint")
	}

	infoURL := endpoint.JoinPath("/mesh/info").String()
	info, err := getJSON[meshInfo](ctx, infoURL)
	if err != nil {
		return ClientConfig{}, fmt.Errorf("cannot read the meshStack instance information: %w", err)
	}
	// A meshStack too old to know about the CLI client answers 200 with these fields absent,
	// which is a far more useful thing to say than a later failure at the authorization
	// endpoint.
	if info.Issuer == "" || info.CliClientId == "" {
		return ClientConfig{}, fmt.Errorf(
			"%s reports no issuer or cliClientId, so this meshStack (version %q) does not support a browser login",
			infoURL, info.Version)
	}

	configURL := strings.TrimSuffix(info.Issuer, "/") + "/.well-known/openid-configuration"
	metadata, err := getJSON[providerMetadata](ctx, configURL)
	if err != nil {
		return ClientConfig{}, fmt.Errorf("cannot read the OpenID configuration of issuer %s: %w", info.Issuer, err)
	}
	if metadata.AuthorizationEndpoint == "" || metadata.TokenEndpoint == "" {
		return ClientConfig{}, fmt.Errorf(
			"%s is missing an authorization_endpoint or a token_endpoint, so it is not a usable OpenID provider", configURL)
	}

	return ClientConfig{
		Endpoint:              endpoint,
		Issuer:                info.Issuer,
		ClientId:              info.CliClientId,
		AuthorizationEndpoint: metadata.AuthorizationEndpoint,
		TokenEndpoint:         metadata.TokenEndpoint,
		EndSessionEndpoint:    metadata.EndSessionEndpoint,
		RevocationEndpoint:    metadata.RevocationEndpoint,
	}, nil
}
