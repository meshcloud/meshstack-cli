package api

import (
	"bytes"
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
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
	"github.com/meshcloud/meshstack-cli/internal/setting"
)

func New() *cobra.Command {
	var flags requestFlags

	cmd := &cobra.Command{
		Use:   "api <path or URL>",
		Short: "Send an authorized request to the meshStack API",
		Long: `Send an authorized request to a path of the meshStack API, and return the response (typically JSON).
The path is relative to the endpoint but can also be an absolute one, as long as a matching profile with authentication credentials can be found.

This exposes everything that the other CLI commands do not cover.
It supports you by taking over versioned content negotiation of meshStack's public API unless you specify it explicitly.

--request-json sends a JSON file, or stdin for '-'. For a path
of no version in the API docs, the Content-Type and Accept headers default to application/json, and
without the API docs the Content-Type alone does.

An answer outside 2xx still writes its body but the command then fails.

Replace the same command line with 'api-docs' in place of 'api' to describe the request.`,
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
			var profileSources setting.Sources
			if r.target.IsAbs() {
				if _, profileSources, err = profileFor(ctx, r.target); err != nil {
					return err
				}
			}
			// Resolved before the API docs, whose first download takes a while, so that a missing
			// login fails at once.
			meshStack, err := internal.ResolveClient(ctx, internal.WithSettingSources(profileSources))
			if err != nil {
				return err
			}
			if r.target.IsAbs() {
				if err = r.cutTo(meshStack.Endpoint); err != nil {
					return err
				}
				selector.Path = r.target.Path
				args = []string{r.target.String()}
			}
			spec, wait, err := flags.loadApiDocs(cmd)
			defer wait()
			if err != nil {
				slog.WarnContext(ctx, fmt.Sprintf("Sending the request as given, without the API docs: %s. "+
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
				if httpErr.IsClientError() && !httpErr.IsUnauthorized() && !httpErr.IsForbidden() && !r.objectNotFound(httpErr) {
					return fmt.Errorf("meshStack answered HTTP %d. Run %s to see what the API takes",
						httpErr.StatusCode, docsCommand(cmd, args))
				}
				// What wraps the HTTP error, such as the auth scope of a 403, stays.
				return errors.New(strings.Replace(err.Error(), httpErr.Error(), fmt.Sprintf("meshStack answered HTTP %d", httpErr.StatusCode), 1))
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

// objectNotFound tells a 404 for a path that exists, whose object is missing, from one for a path that
// does not: only the latter is something the API docs help with.
func (r *request) objectNotFound(httpErr client.HttpError) bool {
	if !httpErr.IsNotFound() || !r.documented {
		return false
	}
	var answer struct {
		ErrorCode string `json:"errorCode"`
	}
	return json.Unmarshal(httpErr.ResponseBody, &answer) != nil || answer.ErrorCode != noHandlerFound
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
	r.documented = err == nil
	if err != nil {
		if !openapi.ParseMediaType(r.header.Get("Accept")).ApiVersion.IsZero() {
			slog.WarnContext(ctx, fmt.Sprintf("Sending the request as given: %s", err))
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
			slog.WarnContext(ctx, fmt.Sprintf("The API docs list no version of %s %s, so no Accept header asks for %s",
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
