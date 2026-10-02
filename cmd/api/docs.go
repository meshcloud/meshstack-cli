package api

import (
	"cmp"
	_ "embed"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	gohttp "net/http"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/meshcloud/meshstack-cli/client/openapi"
	"github.com/meshcloud/meshstack-cli/cmd/internal"
	"github.com/meshcloud/meshstack-cli/cmd/internal/markdown"
)

//go:embed docs.md.tmpl
var docsTemplateText string

var docsTemplate = markdown.Parse("docs", docsTemplateText)

// NewDocs is meshstack api-docs. It breaks the command-tree rule of AGENTS.md and lives in package
// api, to share the request flags of meshstack api.
func NewDocs() *cobra.Command {
	var (
		flags    requestFlags
		output   internal.ShowFlag
		describe string
	)

	cmd := &cobra.Command{
		Use:   "api-docs [<path or URL>]",
		Short: "Show what the meshStack API docs say about a request or a meshObject kind",
		Long: `Show what the meshStack API docs say about a request, given as meshstack api takes it, or about
the operations of a meshObject kind.

Given a path, it describes the operations that apply to the request: the summary, the description,
the parameters and the fields of the request and response bodies of each, as Markdown. Replacing
api by api-docs in a command line of meshstack api describes its request. The operations are those
of every method unless --method names one, each in the latest version it offers unless
--api-version, an Accept or Content-Type header or the body's apiVersion names another. A full URL
is read as the path below the endpoint of the profile that meshstack api would send it with.

Without a path, it lists every operation of the API, or those of the method and version the flags
name.

--describe names a kind as its command does, buildingblock or the alias bb, and optionally one of
its actions after a dot, as in bb.list. It describes each operation of the kind, in the latest
version it offers. It takes no path, and no flag of the request.

--output json writes the part of the OpenAPI document instead: the operations and the schemas they
reference, or the whole document without a path or a flag.

The docs are those of the latest meshStack release, or of meshStack's develop branch with --dev or
for a build of the CLI that is no release. They are kept in the config directory, which must be
writable. ` + "`MESHSTACK_API_DOCS_URL`" + ` names another document, such as that of an older meshStack.`,
		Example: `  meshstack api-docs
  meshstack api-docs -X DELETE
  meshstack api-docs /api/meshobjects/meshtenants/<uuid>
  meshstack api-docs -X POST /api/meshobjects/meshbuildingblocks --api-version v1
  meshstack api-docs -X POST /api/meshobjects/meshbuildingblocks -o json
  meshstack api-docs --describe buildingblock
  meshstack api-docs --describe bb.trigger-run
  meshstack api-docs --describe eventlog.list -o json`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := flags.parse(cmd, args)
			if err != nil {
				return err
			}
			if describe != "" && len(args) > 0 {
				return fmt.Errorf("--%s takes no path, see --help", describeFlagName)
			}
			if r.target.IsAbs() {
				endpoint, _, profileErr := profileFor(cmd.Context(), r.target)
				if profileErr != nil {
					return profileErr
				}
				if endpoint.URL == nil {
					return fmt.Errorf("no stored profile holds %s; give the path relative to the endpoint", r.target.Redacted())
				}
				if err = r.cutTo(endpoint); err != nil {
					return err
				}
			}
			selector, err := flags.selector(r)
			if err != nil {
				return err
			}
			spec, wait, err := flags.loadApiDocs(cmd)
			defer wait()
			if err != nil {
				return err
			}
			if describe != "" {
				return writeKindDescription(cmd, spec, describe, output)
			}
			selected, err := spec.Select(selector)
			if err != nil {
				return err
			}
			if len(selected.Operations) == 0 {
				return noOperationError(selector)
			}
			if selector.Path != "" {
				return writeRequestDescription(cmd, selected, selector.Path, output)
			}
			if output.Json() {
				return writeJson(cmd, selected)
			}
			return markdown.Write(cmd.OutOrStdout(), docsTemplate, pathsOf(selected))
		},
	}

	flags.register(cmd, "", "show only the operations of this HTTP method")
	cmd.Flags().StringVar(&describe, describeFlagName, "", "describe the operations of a meshObject kind, such as buildingblock, or one of them, such as bb.list")
	cmd.MarkFlagsMutuallyExclusive(describeFlagName, "method")
	cmd.MarkFlagsMutuallyExclusive(describeFlagName, "header")
	cmd.MarkFlagsMutuallyExclusive(describeFlagName, "request-json")
	cmd.MarkFlagsMutuallyExclusive(describeFlagName, "api-version")
	output.Register(cmd.Flags())

	return cmd
}

func noOperationError(selector openapi.Selector) error {
	of := strings.TrimSpace(selector.Method + " " + selector.Path)
	if of != "" {
		of = " of " + of
	}
	if !selector.ApiVersion.IsZero() {
		of += " in " + selector.ApiVersion.String()
	}
	return errors.New("the API docs list no operation" + of)
}

// methodOrder puts reading before writing. A method the list leaves out, such as PATCH, goes last.
var methodOrder = []string{gohttp.MethodGet, gohttp.MethodPut, gohttp.MethodPost, gohttp.MethodDelete}

type path struct {
	Template    openapi.PathTemplate
	Methods     []string
	ApiVersions openapi.ApiVersions
}

func pathsOf(spec openapi.Spec) []path {
	var paths []path
	indexOf := map[openapi.PathTemplate]int{}
	for _, operation := range spec.Operations {
		i, ok := indexOf[operation.PathTemplate]
		if !ok {
			i = len(paths)
			indexOf[operation.PathTemplate] = i
			paths = append(paths, path{Template: operation.PathTemplate})
		}
		paths[i].Methods = append(paths[i].Methods, operation.Method)
		paths[i].ApiVersions = append(paths[i].ApiVersions, operation.ApiVersions()...)
	}
	rank := func(method string) int {
		if i := slices.Index(methodOrder, method); i >= 0 {
			return i
		}
		return len(methodOrder)
	}
	for i := range paths {
		slices.SortStableFunc(paths[i].Methods, func(a, b string) int { return cmp.Compare(rank(a), rank(b)) })
		paths[i].ApiVersions = paths[i].ApiVersions.SortedUnique()
	}
	return paths
}

func writeJson(cmd *cobra.Command, value any) error {
	out, err := json.Marshal(value, jsontext.WithIndent("  "))
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(cmd.OutOrStdout(), "%s\n", out)
	return err
}
