// Package apiproxy passes requests on to the meshStack API. It stands apart from internal/tfstate
// because httputil.ReverseProxy takes a transport rather than a client, and forbidigo keeps the
// transport to internal/http and this package.
package apiproxy

import (
	gohttp "net/http"
	"net/http/httputil"
	"net/url"
	"strings"

	"github.com/meshcloud/meshstack-cli/internal/http"
)

// New replaces the Authorization a request came with by that of client, and passes a request
// without one on as it came. The User-Agent names the command first, as it built the request, and
// then the front end of client, through which it went.
func New(client http.AuthorizedClient, endpoint *url.URL, onError func(gohttp.ResponseWriter, *gohttp.Request, error)) gohttp.Handler {
	proxy := &httputil.ReverseProxy{
		Rewrite: func(out *httputil.ProxyRequest) {
			out.SetURL(endpoint)
			out.Out.Header.Set("User-Agent", strings.TrimSpace(out.In.Header.Get("User-Agent")+" "+client.UserAgent.String()))
		},
		Transport:    client.Transport,
		ErrorHandler: onError,
	}
	return gohttp.HandlerFunc(func(w gohttp.ResponseWriter, r *gohttp.Request) {
		if r.Header.Get("Authorization") != "" {
			r = r.Clone(r.Context())
			if err := client.Authorize(r); err != nil {
				onError(w, r, err)
				return
			}
		}
		proxy.ServeHTTP(w, r)
	})
}
