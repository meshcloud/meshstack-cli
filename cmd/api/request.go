package api

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"fmt"
	"io"
	gohttp "net/http"
	"net/url"
	"os"
	"regexp"
	"slices"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/meshcloud/meshstack-cli/client/openapi"
	"github.com/meshcloud/meshstack-cli/client/types/xurl"
	"github.com/meshcloud/meshstack-cli/cmd/internal"
	"github.com/meshcloud/meshstack-cli/internal/apidocs"
)

type requestFlags struct {
	method      string
	headers     []string
	requestJson string
	apiVersion  string
	dev         bool
}

func (f *requestFlags) register(cmd *cobra.Command, defaultMethod, methodUsage string) {
	cmd.Flags().StringVarP(&f.method, "method", "X", defaultMethod, methodUsage)
	cmd.Flags().StringArrayVarP(&f.headers, "header", "H", nil, "add a request header, as 'key: value'")
	cmd.Flags().StringVar(&f.requestJson, "request-json", "", "JSON file to send as the request body, or - for stdin")
	cmd.Flags().StringVar(&f.apiVersion, "api-version", "", "version of the meshObject API, such as v1 or v2-preview, instead of the latest")
	cmd.Flags().BoolVar(&f.dev, "dev", false, "read the API docs of what meshStack's develop branch has merged")
}

type request struct {
	method string
	header gohttp.Header
	// body is nil without --request-json.
	body   jsontext.Value
	target *url.URL
	// documented is set where the API docs hold an operation for the method and path.
	documented bool
}

func (f *requestFlags) parse(cmd *cobra.Command, args []string) (request, error) {
	r := request{method: strings.ToUpper(f.method), header: gohttp.Header{}, target: &url.URL{}}
	for _, line := range f.headers {
		key, value, ok := strings.Cut(line, ":")
		if !ok || strings.TrimSpace(key) == "" {
			return r, fmt.Errorf("header '%s' is not of 'key: value' format", line)
		}
		r.header.Add(strings.TrimSpace(key), strings.TrimSpace(value))
	}

	var err error
	switch f.requestJson {
	case "":
	case "-":
		r.body, err = io.ReadAll(cmd.InOrStdin())
	default:
		r.body, err = os.ReadFile(f.requestJson)
	}
	if err != nil {
		return r, err
	}
	if r.body != nil && !r.body.IsValid() {
		return r, fmt.Errorf("--request-json %s is no JSON", f.requestJson)
	}

	if len(args) > 0 {
		if r.target, err = url.Parse(args[0]); err != nil {
			return r, err
		}
		if r.target.Host != "" && !r.target.IsAbs() {
			return r, fmt.Errorf("'%s' names a host but no scheme; give a full URL or a path relative to the endpoint", args[0])
		}
	}
	return r, nil
}

func (r *request) cutTo(endpoint xurl.URL) error {
	path, held := endpoint.PathTo(r.target)
	if !held {
		return fmt.Errorf("endpoint %s does not hold %s", endpoint, r.target.Redacted())
	}
	r.target = &url.URL{Path: path, RawQuery: r.target.RawQuery}
	return nil
}

func (f *requestFlags) selector(r request) (openapi.Selector, error) {
	selector := openapi.Selector{Method: r.method, Path: r.target.Path}
	var given []string
	for _, header := range []string{"Accept", "Content-Type"} {
		if version := openapi.ParseMediaType(r.header.Get(header)).ApiVersion; !version.IsZero() {
			given = append(given, version.String())
		}
	}
	if f.apiVersion != "" {
		given = append(given, f.apiVersion)
	}
	if r.body.Kind() == '{' {
		var body struct {
			ApiVersion string `json:"apiVersion"`
		}
		if err := json.Unmarshal(r.body, &body); err != nil {
			return selector, err
		}
		if body.ApiVersion != "" {
			given = append(given, body.ApiVersion)
		}
	}
	if len(slices.Compact(slices.Clone(given))) > 1 {
		return selector, fmt.Errorf("the Accept and Content-Type headers, --api-version and the body's apiVersion ask for %s", strings.Join(given, ", "))
	}
	if len(given) > 0 {
		var err error
		if selector.ApiVersion, err = openapi.ParseApiVersion(given[0]); err != nil {
			return selector, err
		}
	}
	return selector, nil
}

func (f *requestFlags) loadApiDocs(cmd *cobra.Command) (openapi.Spec, func(), error) {
	httpClient, err := internal.ResolveClientOptions().HttpClient()
	if err != nil {
		return openapi.Spec{}, func() {}, err
	}
	return apidocs.Load(cmd.Context(), apidocs.Options{
		Sources: internal.SettingSources(),
		Client:  httpClient,
		Version: internal.Version,
		Dev:     f.dev,
	})
}

// docsCommand names the method even where meshstack api took GET by default, because api-docs takes
// no method as every method. It leaves out a body read from stdin, which the request used up.
func docsCommand(cmd *cobra.Command, args []string) string {
	words := []string{"meshstack", "api-docs"}
	words = append(words, args...)
	if method := cmd.Flags().Lookup("method"); !method.Changed {
		words = append(words, "-X", method.Value.String())
	}
	cmd.Flags().Visit(func(flag *pflag.Flag) {
		if flag.Name == "request-json" && flag.Value.String() == "-" {
			return
		}
		name := "--" + flag.Name
		if flag.Shorthand != "" {
			name = "-" + flag.Shorthand
		}
		if flag.Value.Type() == "bool" {
			words = append(words, name)
			return
		}
		if values, ok := flag.Value.(pflag.SliceValue); ok {
			for _, value := range values.GetSlice() {
				words = append(words, name, value)
			}
			return
		}
		words = append(words, name, flag.Value.String())
	})
	for i, word := range words {
		words[i] = shellQuote(word)
	}
	return strings.Join(words, " ")
}

var shellSafeRe = regexp.MustCompile(`^[A-Za-z0-9_/.:=@,+-]+$`)

func shellQuote(word string) string {
	if shellSafeRe.MatchString(word) {
		return word
	}
	return "'" + strings.ReplaceAll(word, "'", `'\''`) + "'"
}
