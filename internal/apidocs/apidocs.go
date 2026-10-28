// Package apidocs loads the OpenAPI document of the meshStack API into the config directory, and
// keeps it there up to date.
package apidocs

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	gourl "net/url"
	"os"
	"strings"

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

// urlEnv names the document of the fake meshStack that the tests and the demo serve, or as a file://
// URL the one the acceptance suite generates from the meshStack it runs against. It is no
// setting.Setting, and no help names it, so that nobody else comes to rely on it.
const urlEnv = "MESHSTACK_API_DOCS_URL"

type Options struct {
	setting.Sources

	Client   http.Client
	Endpoint xurl.URL
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
	customEnv := os.Getenv(urlEnv)
	if path, isFile := strings.CutPrefix(customEnv, "file://"); isFile {
		document, readErr := os.ReadFile(path) //nolint:gosec // G304: the path is the one the acceptance suite sets
		if readErr != nil {
			return spec, wait, fmt.Errorf("%s: %w", urlEnv, readErr)
		}
		if spec, err = openapi.Parse(bytes.NewReader(document)); err != nil {
			return spec, wait, fmt.Errorf("%s: %w", path, err)
		}
		return spec, wait, nil
	}
	dir, err := opts.ResolveSetting(ctx, config.DirectorySetting)
	if err != nil {
		return spec, wait, err
	}
	if mkdirErr := os.MkdirAll(string(dir), 0o700); mkdirErr != nil || !writable(dir) {
		return spec, wait, fmt.Errorf("%w: %s is not writable, set %s to a directory that is",
			ErrNoConfigDir, dir, config.DirectorySetting.EnvKey())
	}

	if customEnv != "" {
		var custom xurl.URL
		if err = custom.UnmarshalText([]byte(customEnv)); err != nil {
			return spec, wait, fmt.Errorf("%s: %w", urlEnv, err)
		}
		return opts.download(ctx, custom.URL, dir.Join(fmt.Sprintf("api-docs-custom-%s.json", urlHash(custom.URL))), true)
	}
	if opts.Endpoint.URL != nil && !opts.Dev {
		served := opts.Endpoint.JoinPath("api", "openapi")
		spec, wait, err = opts.download(ctx, served, dir.Join(fmt.Sprintf("api-docs-%s.json", urlHash(served))), true)
		if httpErr, ok := errors.AsType[http.Error](err); !ok || (!httpErr.IsUnauthorized() && !httpErr.IsNotFound()) {
			return spec, wait, err
		}
		slog.DebugContext(ctx, fmt.Sprintf("%s serves no API docs at /api/openapi, reading those of the docs site", opts.Endpoint))
	}

	url, path, conditionalGet := releasedUrl, dir.Join("api-docs.json"), false
	if cliVersion, parseErr := version.Parse(opts.Version); opts.Dev || parseErr != nil || !cliVersion.IsRelease() {
		url, path, conditionalGet = devUrl, dir.Join("api-docs-dev.json"), true
	}
	return opts.download(ctx, url.URL, path, conditionalGet)
}

// urlHash names one file per URL, because the conditional GET asks only for a document newer than the
// stored one: after a switch to the docs of an older meshStack, it would keep those of the newer one.
func urlHash(url *gourl.URL) string {
	sum := sha256.Sum256([]byte(url.String()))
	return hex.EncodeToString(sum[:6])
}

func (opts Options) download(ctx context.Context, url *gourl.URL, path string, conditionalGet bool) (spec openapi.Spec, wait func(), err error) {
	wait, err = opts.Client.Download(ctx, url, path, http.WithConditionalGet(conditionalGet))
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
