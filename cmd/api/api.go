package api

import (
	"bytes"
	"context"
	"encoding/json/jsontext"
	"errors"
	"fmt"
	"io"
	"log/slog"
	gohttp "net/http"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/spf13/cobra"

	"github.com/meshcloud/meshstack-cli/client"
	"github.com/meshcloud/meshstack-cli/client/openapi"
	"github.com/meshcloud/meshstack-cli/cmd/internal"
	"github.com/meshcloud/meshstack-cli/internal/http"
)

func New() *cobra.Command {
	var flags requestFlags

	cmd := &cobra.Command{
		Use:   "api <path or URL>",
		Short: "Send an authorized request to the meshStack API",
		Long: fmt.Sprintf(`Send an authorized request to a path of the meshStack API, and print the response, which is typically JSON. Give the path relative to the endpoint, or give a full URL, whose scheme and host the command then uses as the endpoint.

Use this command for everything that the other commands do not cover. For a path of meshStack's public API, the command sets the Accept and Content-Type headers to the latest API version that the API docs list, unless you set them yourself.

--%[1]s sends a JSON file as the request body, or stdin for '-'. If the API docs list no version for the path, the command sets the Accept and Content-Type headers to application/json. If the command cannot download the API docs, it sets only the Content-Type header to application/json.

If the response status is not 2xx, the command still prints the response body, and then fails.

To see what the API docs say about the request, run the same command line with 'api' replaced by 'api-docs'.`, requestJsonFlagName),
		Example: `  meshstack api '/api/meshobjects/meshtenants?workspaceIdentifier=my-workspace'
  meshstack api -X DELETE /api/meshobjects/meshbuildingblocks/<uuid>/purge
  meshstack api -X POST /api/meshobjects/meshworkspaces --request-json workspace.json
  meshstack api https://meshstack.example.com/api/meshobjects/meshworkspaces/my-workspace`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := flags.parse(cmd, args)
			if err != nil {
				return err
			}
			selector, err := flags.selector(r)
			if err != nil {
				return err
			}
			ctx := cmd.Context()
			// Resolved before the API docs, whose first download takes a while, so that a missing
			// login fails at once.
			meshStack, err := internal.ResolveClient(ctx, internal.WithSettingSources(r.originAsEndpoint()))
			if err != nil {
				return err
			}
			if r.target.IsAbs() && !strings.EqualFold(r.origin(), meshStack.Endpoint.String()) {
				return fmt.Errorf("%s is not at the endpoint %s of this command; leave out --%s", r.target.Redacted(), meshStack.Endpoint, internal.EndpointFlag.Name)
			}
			spec, wait, err := flags.loadApiDocs(cmd, meshStack.Endpoint)
			defer wait()
			if err != nil {
				slog.WarnContext(ctx, fmt.Sprintf("Sending the request unchanged, because the API docs are not available: %s. "+
					"Run %s to see why.", err, docsCommand(cmd, args)))
			} else if negotiateErr := r.negotiate(ctx, spec, selector); negotiateErr != nil {
				return negotiateErr
			}
			if _, typed := r.header["Content-Type"]; r.body != nil && !typed {
				r.header.Set("Content-Type", "application/json")
			}

			answer, err := meshStack.Raw.DoRequest(ctx, r.method, r.target.Path,
				http.WithUrlQuery(r.target.Query()), http.WithHeaders(r.header), http.WithBody(r.body))
			if httpErr, ok := errors.AsType[client.HttpError](err); ok {
				if writeErr := writeAnswer(cmd.OutOrStdout(), httpErr.ResponseBody); writeErr != nil {
					return writeErr
				}
				// The body is on stdout already, so the error says only what it adds.
				if r.docsHelpWith(httpErr) {
					return withHint(fmt.Sprintf("%s. Run %s to see what the API expects",
						httpErr.Summary(), docsCommand(cmd, args)), httpErr)
				}
				// What wraps the HTTP error, such as the auth scope of a 403, stays.
				return withHint(strings.Replace(err.Error(), httpErr.Error(), httpErr.Summary(), 1), httpErr)
			}
			if err != nil {
				return err
			}
			return writeAnswer(cmd.OutOrStdout(), answer)
		},
	}

	flags.register(cmd, gohttp.MethodGet, "HTTP method of the request")

	return cmd
}

// docsHelpWith is false for a 404 of a path meshStack serves nothing at, and of an object missing at a
// documented path: the API docs say nothing about either.
func (r *request) docsHelpWith(httpErr client.HttpError) bool {
	switch {
	case !httpErr.IsClientError(), httpErr.IsUnauthorized(), httpErr.IsForbidden():
		return false
	case httpErr.IsNotFound():
		return !r.documented && httpErr.ErrorCode() != noHandlerFound
	default:
		return true
	}
}

// withHint adds the hint on a line of its own. Stdout carries the raw body, which the error does not
// repeat.
func withHint(message string, httpErr client.HttpError) error {
	if hint := httpErr.ClientVersionHint(); hint != "" {
		message += "\n" + hint
	}
	return errors.New(message)
}

// noHandlerFound is the errorCode of meshStack's answer to a path it serves nothing at.
const noHandlerFound = "NoHandlerFound"

// writeAnswer ends a body of text with a newline, so that the error after it starts on a line of its
// own.
func writeAnswer(w io.Writer, body []byte) error {
	if len(body) == 0 {
		return nil
	}
	if value := jsontext.Value(slices.Clone(body)); value.Indent(jsontext.WithIndent("  ")) == nil {
		body = value
	}
	if utf8.Valid(body) && !bytes.HasSuffix(body, []byte("\n")) {
		body = append(body, '\n')
	}
	_, err := w.Write(body)
	return err
}

func (r *request) negotiate(ctx context.Context, spec openapi.Spec, selector openapi.Selector) error {
	// The request goes out as given where the docs know no better, because they can lag behind the
	// meshStack it is sent to.
	selected, err := spec.Select(selector)
	r.documented = err == nil && len(selected.Operations) > 0
	if err != nil {
		if !openapi.ParseMediaType(r.header.Get("Accept")).ApiVersion.IsZero() {
			slog.WarnContext(ctx, fmt.Sprintf("Sending the request unchanged: %s", err))
			return nil
		}
		return err
	}
	var mediaType openapi.MediaType
	for _, operation := range selected.Operations {
		if latest, ok := operation.LatestMediaType(); ok && (mediaType.Name == "" || !latest.ApiVersion.IsZero()) {
			mediaType = latest
		}
	}
	if mediaType.ApiVersion.IsZero() {
		if !selector.ApiVersion.IsZero() && r.header.Get("Accept") == "" {
			slog.WarnContext(ctx, fmt.Sprintf("The API docs have no version of %s %s, so the request gets no Accept header for %s",
				selector.Method, selector.Path, selector.ApiVersion))
		} else {
			slog.DebugContext(ctx, fmt.Sprintf("The API docs list no version of %s %s", selector.Method, selector.Path))
		}
		// Only here, because a meshObject endpoint answers application/json with a 406, which
		// does not say, as the 406 to no Accept header does, that it wants a version.
		if r.header.Get("Accept") == "" {
			if mediaType.Name != "" {
				r.header.Set("Accept", mediaType.Name)
			} else if r.body != nil {
				r.header.Set("Accept", "application/json")
			}
		}
		return nil
	}
	slog.DebugContext(ctx, fmt.Sprintf("Sending %s %s as %s", selector.Method, selector.Path, mediaType.Name))
	if r.header.Get("Accept") == "" {
		r.header.Set("Accept", mediaType.Name)
	}
	if r.body == nil {
		return nil
	}
	if r.header.Get("Content-Type") == "" {
		r.header.Set("Content-Type", mediaType.Name)
	}
	r.body, err = mediaType.WithKindAndApiVersion(r.body)
	return err
}
