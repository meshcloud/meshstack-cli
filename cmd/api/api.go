package api

import (
	"errors"
	"fmt"
	"io"
	gohttp "net/http"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/meshcloud/meshstack-cli/client"
	"github.com/meshcloud/meshstack-cli/cmd/internal"
)

func New() *cobra.Command {
	var (
		method  string
		headers []string
		input   string
	)

	cmd := &cobra.Command{
		Use:   "api <path>",
		Short: "Send an authorized request to the meshStack API",
		Long: `Send an authorized request to a path of the meshStack API, and write the answer as it came.

This reaches what the other commands do not cover, such as deleting a meshObject or reading a newer
representation of it. meshStack versions an endpoint through the Accept header, so name the media
type there. The meshStack OpenAPI spec lists the paths and their media types:
https://docs.meshcloud.io/api/meshstack-openapi-docs.json

--input sends a file, or stdin for '-', as the request body, typed application/json unless a
Content-Type header says otherwise.

An answer outside 2xx still writes its body, and the command then fails.`,
		Example: `  meshstack api '/api/meshobjects/meshtenants?workspaceIdentifier=my-workspace' \
    -H 'Accept: application/vnd.meshcloud.api.meshtenant.v4.hal+json'
  meshstack api -X DELETE /api/meshobjects/meshbuildingblocks/<uuid>/purge \
    -H 'Accept: application/vnd.meshcloud.api.meshbuildingblock.v2-preview.hal+json'`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			header, err := parseHeaders(headers)
			if err != nil {
				return err
			}
			body, err := readInput(cmd, input)
			if err != nil {
				return err
			}
			if _, typed := header["Content-Type"]; body != nil && !typed {
				header.Set("Content-Type", "application/json")
			}

			ctx := cmd.Context()
			session, err := internal.ResolveSession(ctx)
			if err != nil {
				return err
			}
			answer, err := session.Api(ctx, strings.ToUpper(method), args[0], header, body)
			if _, writeErr := cmd.OutOrStdout().Write(answer); writeErr != nil {
				return writeErr
			}
			// The body is on stdout already, so the error says only what it adds.
			if httpErr, ok := errors.AsType[client.HttpError](err); ok {
				return fmt.Errorf("meshStack answered HTTP %d", httpErr.StatusCode)
			}
			return err
		},
	}

	cmd.Flags().StringVarP(&method, "method", "X", gohttp.MethodGet, "HTTP method of the request")
	cmd.Flags().StringArrayVarP(&headers, "header", "H", nil, "add a request header, as 'key: value'")
	cmd.Flags().StringVar(&input, "input", "", "file to send as the request body, or - for stdin")

	return cmd
}

func parseHeaders(headers []string) (gohttp.Header, error) {
	header := gohttp.Header{}
	for _, line := range headers {
		key, value, ok := strings.Cut(line, ":")
		if !ok || strings.TrimSpace(key) == "" {
			return nil, fmt.Errorf("header '%s' is not of 'key: value' format", line)
		}
		header.Add(strings.TrimSpace(key), strings.TrimSpace(value))
	}
	return header, nil
}

func readInput(cmd *cobra.Command, input string) ([]byte, error) {
	switch input {
	case "":
		return nil, nil
	case "-":
		return io.ReadAll(cmd.InOrStdin())
	default:
		//nolint:gosec // G304: reading the file the user named is what --input is for
		return os.ReadFile(input)
	}
}
