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
	"github.com/meshcloud/meshstack-cli/client/types/xurl"
	"github.com/meshcloud/meshstack-cli/cmd/internal"
	"github.com/meshcloud/meshstack-cli/cmd/internal/markdown"
)

//go:embed docs.md.tmpl
var docsTemplateText string

var docsTemplate = markdown.Parse("docs", docsTemplateText)

// NewDocs is meshstack api-docs. It breaks the command-tree rule of the development skill and lives
// in package api, to share the request flags of meshstack api.
func NewDocs() *cobra.Command {
	var (
		flags    requestFlags
		output   internal.ShowFlag
		describe string
	)

	cmd := &cobra.Command{
		Use:   "api-docs [<path or URL>]",
		Short: "Show what the meshStack API docs say about a request or a meshObject kind",
		Long: fmt.Sprintf(`Show what the meshStack API docs say about a request or about a meshObject kind.

Give the request as you would give it to 'meshstack api': a path or a URL, with --%[1]s, --%[2]s, --%[3]s and --%[4]s if needed. Without a path, the command lists every operation of the API. With --%[1]s or --%[4]s, it lists only the operations of that method or version.

--%[5]s describes every operation of a meshObject kind, in the latest version of the kind. Give the kind by its command name, such as buildingblock, or by its alias, such as bb. To describe one action of the kind, add the action after a dot, as in bb.list. --%[5]s cannot be combined with a path or with the flags of a request.

--%[6]s %[7]s prints the matching part of the OpenAPI document as JSON.

The command downloads the API docs from your meshStack, so they list only what your meshStack offers. If no endpoint is set, or if your meshStack is too old to serve its API docs, the command uses the API docs of the latest release on docs.meshcloud.io.

--%[8]s uses the API docs of meshStack's development version instead.`,
			methodFlagName, headerFlagName, requestJsonFlagName, apiVersionFlagName, describeFlagName,
			internal.OutputFlagName, internal.OutputJson, devFlagName),
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
				return fmt.Errorf("--%s cannot be combined with a path; see --help", describeFlagName)
			}
			var endpoint xurl.URL
			if r.target.IsAbs() {
				if endpoint, _, err = profileFor(cmd.Context(), r.target); err != nil {
					return err
				}
				if endpoint.URL == nil {
					return fmt.Errorf("no stored profile matches %s; give the path relative to the endpoint", r.target.Redacted())
				}
				if err = r.cutTo(endpoint); err != nil {
					return err
				}
			} else if endpoint, err = docsEndpoint(cmd.Context()); err != nil {
				return err
			}
			selector, err := flags.selector(r)
			if err != nil {
				return err
			}
			spec, wait, err := flags.loadApiDocs(cmd, endpoint)
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
	cmd.MarkFlagsMutuallyExclusive(describeFlagName, methodFlagName)
	cmd.MarkFlagsMutuallyExclusive(describeFlagName, headerFlagName)
	cmd.MarkFlagsMutuallyExclusive(describeFlagName, requestJsonFlagName)
	cmd.MarkFlagsMutuallyExclusive(describeFlagName, apiVersionFlagName)
	output.Register(cmd.Flags())

	return cmd
}

func noOperationError(selector openapi.Selector) error {
	of := strings.TrimSpace(selector.Method + " " + selector.Path)
	if of != "" {
		of = " for " + of
	}
	if !selector.ApiVersion.IsZero() {
		of += " in " + selector.ApiVersion.String()
	}
	return errors.New("the API docs have no operation" + of)
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
