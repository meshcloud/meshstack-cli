// Package apidocs loads the OpenAPI document of the meshStack API into the config directory, and
// keeps it there up to date.
package apidocs

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"

	"github.com/meshcloud/meshstack-cli/client/openapi"
	"github.com/meshcloud/meshstack-cli/client/types/xurl"
	"github.com/meshcloud/meshstack-cli/internal/config"
	"github.com/meshcloud/meshstack-cli/internal/http"
	"github.com/meshcloud/meshstack-cli/internal/setting"
	"github.com/meshcloud/meshstack-cli/internal/version"
)

var (
	releasedUrl = xurl.MustParsef("https://docs.meshcloud.io/api/meshstack-openapi-docs.json")
	devUrl      = xurl.MustParsef("https://docs.dev.meshcloud.io/api/meshstack-openapi-docs.json")
)

// urlEnv names the document of the fake meshStack that the tests and the demo serve. It is no
// setting.Setting, and no help names it, so that nobody else comes to rely on it.
const urlEnv = "MESHSTACK_API_DOCS_URL"

type Options struct {
	setting.Sources

	Client http.Client
	// Version is the CLI's. A build of no release reads the docs of develop, as Dev does.
	Version string
	Dev     bool
}

// ErrNoConfigDir is returned rather than a document downloaded for nothing, because 4.7MB per
// command is too much to fetch again and again.
var ErrNoConfigDir = errors.New("no writable config directory to keep the API docs in")

// Load returns a wait that the caller runs when the command has done its work, so that a slow
// download can finish.
func Load(ctx context.Context, opts Options) (spec openapi.Spec, wait func(), err error) {
	wait = func() {}
	dir, err := opts.ResolveSetting(ctx, config.DirectorySetting)
	if err != nil {
		return spec, wait, err
	}
	if mkdirErr := os.MkdirAll(string(dir), 0o700); mkdirErr != nil || !writable(dir) {
		return spec, wait, fmt.Errorf("%w: %s is not writable, set %s to a directory that is",
			ErrNoConfigDir, dir, config.DirectorySetting.EnvKey())
	}

	url, path, conditionalGet := releasedUrl, dir.Join("api-docs.json"), false
	if cliVersion, parseErr := version.Parse(opts.Version); opts.Dev || parseErr != nil || !cliVersion.IsRelease() {
		url, path, conditionalGet = devUrl, dir.Join("api-docs-dev.json"), true
	}
	if customEnv := os.Getenv(urlEnv); customEnv != "" {
		var custom xurl.URL
		if err = custom.UnmarshalText([]byte(customEnv)); err != nil {
			return spec, wait, fmt.Errorf("%s: %w", urlEnv, err)
		}
		// One file per URL, because the conditional GET asks only for a document newer than the stored
		// one: after a switch to the docs of an older meshStack, it would keep those of the newer one.
		urlHash := sha256.Sum256([]byte(custom.String()))
		url, path, conditionalGet = custom, dir.Join(fmt.Sprintf("api-docs-custom-%x.json", urlHash[:6])), true
	}

	wait, err = opts.Client.Download(ctx, url.URL, path, http.WithConditionalGet(conditionalGet))
	if err != nil {
		return spec, wait, fmt.Errorf("cannot download the API docs from %s: %w", url, err)
	}
	document, err := os.ReadFile(path) //nolint:gosec // G304: the path is the config directory's, joined above
	if err != nil {
		return spec, wait, err
	}
	spec, err = openapi.Parse(bytes.NewReader(document))
	if err != nil {
		// Removed, because a conditional GET would keep it for good: a captive portal's page, say,
		// stored with the time of its download, is newer than the document.
		return spec, wait, fmt.Errorf("%s: %w, so it is removed for the next command to download again",
			path, errors.Join(err, os.Remove(path)))
	}
	return spec, wait, nil
}

func writable(dir config.Directory) bool {
	if !dir.Exists() {
		return false
	}
	probe, err := os.CreateTemp(string(dir), ".writable-*")
	if err != nil {
		return false
	}
	_ = probe.Close()
	return os.Remove(probe.Name()) == nil
}
