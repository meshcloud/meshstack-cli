package generator

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"maps"
	"net/url"
	"os"
	"path/filepath"
	"slices"

	"github.com/meshcloud/meshstack-cli/client/openapi"
	"github.com/meshcloud/meshstack-cli/internal/http"
)

// Main generates into the directory of Config.Module, which -out names and which is the working
// directory by default, as go:generate runs a command in the directory of its file. The API docs
// come from -spec, a path or URL, which defaults to the docs of meshStack's develop branch.
func Main(cfg Config) {
	spec := flag.String("spec", openapi.DevUrl.String(), "path or URL of the meshStack API docs")
	out := flag.String("out", ".", "directory of "+cfg.Module)
	flag.Parse()
	if err := run(context.Background(), cfg, *spec, *out); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context, cfg Config, source, out string) error {
	document, err := load(ctx, source)
	if err != nil {
		return fmt.Errorf("cannot read the API docs from %s: %w", source, err)
	}
	spec, err := openapi.Parse(bytes.NewReader(document))
	if err != nil {
		return err
	}
	files, err := Generate(spec, cfg)
	if err != nil {
		return err
	}
	for _, name := range slices.Sorted(maps.Keys(files)) {
		target := filepath.Join(out, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(target), 0o750); err != nil {
			return err
		}
		if err := os.WriteFile(target, files[name], 0o600); err != nil {
			return err
		}
	}
	return nil
}

func load(ctx context.Context, source string) ([]byte, error) {
	parsed, err := url.Parse(source)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return os.ReadFile(source) //nolint:gosec // G304: the developer names the file to generate from
	}
	client := http.NewClient(http.UserAgent{GitHubRepo: "meshcloud/meshstack-cli", Version: "generator"})
	return client.DoRequest[[]byte](ctx, http.MethodGet, parsed)
}
