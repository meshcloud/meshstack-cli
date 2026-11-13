package apidocs

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	gohttp "net/http"
	"os"
	"time"

	"github.com/meshcloud/meshstack-cli/client/http"
	"github.com/meshcloud/meshstack-cli/client/openapi"
	"github.com/meshcloud/meshstack-cli/client/types/version"
	"github.com/meshcloud/meshstack-cli/client/types/xurl"
	"github.com/meshcloud/meshstack-cli/internal/config"
	"github.com/meshcloud/meshstack-cli/internal/setting"
)

var (
	releasedUrl = xurl.MustParsef("https://docs.meshcloud.io/api/meshstack-openapi-docs.json")
	devUrl      = xurl.MustParsef("https://docs.dev.meshcloud.io/api/meshstack-openapi-docs.json")
)

// requestTimeout is short, because the stored or the published docs can stand in.
const requestTimeout = 15 * time.Second

type Options struct {
	setting.Sources

	Client   http.Client
	Endpoint xurl.URL
	// Dev and a Version of no release fall back to the published docs of develop.
	Version string
	Dev     bool
}

var ErrNoConfigDir = errors.New("no writable config directory to keep the API docs in")

func Load(ctx context.Context, opts Options) (openapi.Spec, error) {
	if opts.Endpoint.URL == nil {
		return opts.loadPublished(ctx, nil)
	}
	dir, err := opts.ResolveSetting(ctx, config.DirectorySetting)
	if err != nil {
		return openapi.Spec{}, err
	}
	s := store{dir: dir.Join("api-docs")}
	if mkdirErr := os.MkdirAll(s.indexDir(), 0o700); mkdirErr != nil || !writable(s.indexDir()) {
		return openapi.Spec{}, fmt.Errorf("%w: %s is not writable, set %s to a directory that is",
			ErrNoConfigDir, dir, config.DirectorySetting.EnvKey())
	}

	spec, err := s.load(ctx, opts.Client, opts.Endpoint)
	if _, ok := errors.AsType[fallbackError](err); ok {
		return opts.loadPublished(ctx, err)
	}
	return spec, err
}

func (opts Options) loadPublished(ctx context.Context, why error) (openapi.Spec, error) {
	url := releasedUrl
	if cliVersion, parseErr := version.Parse(opts.Version); opts.Dev || parseErr != nil || !cliVersion.IsRelease() {
		url = devUrl
	}
	if why != nil {
		slog.WarnContext(ctx, fmt.Sprintf("%s, so this command reads the API docs from %s, which may not match it", why, url))
	}
	document, _, err := download(ctx, opts.Client, url)
	if err != nil {
		return openapi.Spec{}, fmt.Errorf("cannot download the API docs from %s: %w", url, err)
	}
	return parse(url, document)
}

func download(ctx context.Context, client http.Client, url xurl.URL, options ...http.RequestOption) ([]byte, gohttp.Header, error) {
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	var header gohttp.Header
	// No option sets an Accept-Encoding, so that net/http asks for gzip and decompresses the answer.
	document, err := http.ReadAllBody(client.DoRequest(ctx, http.MethodGet, url.URL,
		append(options, http.Retryable(0), http.WithCapturedResponseHeader(&header))...))
	return document, header, err
}

func parse(url xurl.URL, document []byte) (openapi.Spec, error) {
	spec, err := openapi.Parse(bytes.NewReader(document))
	if err != nil {
		return spec, fmt.Errorf("the API docs from %s: %w", url, err)
	}
	return spec, nil
}

func writable(dir string) bool {
	probe, err := os.CreateTemp(dir, ".writable-*")
	if err != nil {
		return false
	}
	_ = probe.Close()
	return os.Remove(probe.Name()) == nil
}
