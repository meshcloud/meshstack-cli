package auth

import (
	"context"
	"fmt"
	gohttp "net/http"
	"net/url"

	"github.com/meshcloud/meshstack-cli/client"
	"github.com/meshcloud/meshstack-cli/internal/auth"
	"github.com/meshcloud/meshstack-cli/internal/http"
)

type (
	// ResolveClientOptions carries the setting sources and information about the calling frontend.
	ResolveClientOptions = auth.ResolveSessionOptions
)

// ResolveClient resolves a session and builds its client in one call.
func ResolveClient(ctx context.Context, opts ResolveClientOptions) (client.Client, error) {
	session, err := auth.ResolveSession(ctx, opts)
	if err != nil {
		return client.Client{}, err
	}
	return session.Client()
}

// Api resolves a session and sends one request to a path of its endpoint, and returns the answer
// unparsed. A non-2xx answer returns its body together with a [client.HttpError].
func Api(ctx context.Context, opts ResolveClientOptions, method string, pathAndQuery string, header gohttp.Header, body []byte) ([]byte, error) {
	target, err := url.Parse(pathAndQuery)
	if err != nil {
		return nil, err
	}
	session, err := auth.ResolveSession(ctx, opts)
	if err != nil {
		return nil, err
	}
	// The bearer token must never leave for another host.
	if target.Scheme != "" || target.Host != "" {
		return nil, fmt.Errorf("'%s' is not a path, write it relative to the endpoint %s", pathAndQuery, session.CurrentProfile.Endpoint)
	}
	apiClient, err := session.ApiClient()
	if err != nil {
		return nil, err
	}
	requestUrl := session.CurrentProfile.Endpoint.JoinPath(target.Path)
	requestUrl.RawQuery = target.RawQuery
	return apiClient.DoRawRequest(ctx, method, requestUrl, http.WithHeaders(header), http.WithBody(body))
}
