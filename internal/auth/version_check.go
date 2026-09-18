package auth

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"time"

	"github.com/meshcloud/meshstack-cli/client/types/xurl"
	"github.com/meshcloud/meshstack-cli/internal/config"
	"github.com/meshcloud/meshstack-cli/internal/http"
	"github.com/meshcloud/meshstack-cli/internal/json"
	"github.com/meshcloud/meshstack-cli/internal/version"
)

const (
	// checkInterval is recorded on disk, as one invocation is too short-lived to hold it.
	checkInterval = 24 * time.Hour
	// releaseCheckTimeout keeps an unreachable GitHub from holding up the caller's own work.
	releaseCheckTimeout = 10 * time.Second
)

// warnIfNewerReleasePresent logs a warning when GitHub serves a newer release of the calling front
// end. It lives here rather than in internal/version, which the depguard rule in .golangci.yml
// keeps closed to everything but the standard library.
func warnIfNewerReleasePresent(ctx context.Context, configDir config.Directory, client http.Client, opts ResolveSessionOptions) (err error) {
	currentVersion, err := version.Parse(opts.Version)
	if err != nil {
		slog.DebugContext(ctx, fmt.Sprintf("Skipping release check for build with unparsable version: %s", err))
		return nil
	}
	if !configDir.Exists() {
		slog.DebugContext(ctx, fmt.Sprintf("Skipping release check as config dir %s does not exist", configDir))
		return nil
	}
	lastCheckFile := configDir.VersionCheckJson()
	versionCheck := struct {
		LastCheck time.Time `json:"lastCheck"`
	}{}
	if loadErr := json.UnmarshalFrom(ctx, lastCheckFile, &versionCheck); loadErr != nil && !errors.Is(loadErr, fs.ErrNotExist) {
		return loadErr
	}
	if durationUntilNextCheck := checkInterval - time.Since(versionCheck.LastCheck); durationUntilNextCheck > 0 {
		slog.DebugContext(ctx, fmt.Sprintf("Skipping release check, next one due in %s", durationUntilNextCheck))
		return nil
	}
	// Recorded even when the request below fails, so an unreachable GitHub is asked once a day.
	versionCheck.LastCheck = time.Now()
	defer func() {
		err = errors.Join(err, json.MarshalTo(ctx, lastCheckFile, versionCheck))
	}()

	ctxRequest, cancel := context.WithTimeout(ctx, releaseCheckTimeout)
	defer cancel()
	apiUrl := xurl.MustParsef("https://api.github.com/repos/%s/releases/latest", opts.GitHubRepo)
	latestRelease, err := client.DoRequest[struct {
		TagName string `json:"tag_name"`
	}](ctxRequest, http.MethodGet, apiUrl.URL, http.Retryable())
	if httpError, ok := errors.AsType[http.Error](err); ok && httpError.IsNotFound() {
		return fmt.Errorf("no latest release found at %s: %w", apiUrl, err)
	} else if err != nil {
		return fmt.Errorf("cannot fetch %s: %w", apiUrl, err)
	}
	latestReleaseVersion, err := version.Parse(latestRelease.TagName)
	if err != nil {
		return fmt.Errorf("cannot parse the latest release from %s: %w", apiUrl, err)
	}
	if currentVersion.Less(latestReleaseVersion) {
		releaseUrl := xurl.MustParsef("https://github.com/%s/releases/latest", opts.GitHubRepo)
		slog.WarnContext(ctx, fmt.Sprintf("Please download the latest release %s, newer than %s, at %s",
			latestReleaseVersion, currentVersion, releaseUrl))
	}
	return nil
}
