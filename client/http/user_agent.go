package http

import (
	"fmt"
	"strings"
)

// UserAgent names the front end that sends a request, as "<repo>/<version>", such as
// meshstack-cli/v0.9.0.
type UserAgent struct {
	// GitHubRepo is "<org>/<repo>", whose releases are checked for a newer one.
	GitHubRepo string
	Version    string
}

func (u UserAgent) Validate() error {
	org, repo, ok := strings.Cut(u.GitHubRepo, "/")
	if !ok || org == "" || repo == "" {
		return fmt.Errorf("GitHub repo '%s' is not of <org>/<repo> format", u.GitHubRepo)
	}
	if u.Version == "" {
		return fmt.Errorf("no version given for GitHub repo '%s'", u.GitHubRepo)
	}
	return nil
}

func (u UserAgent) String() string {
	_, repo, _ := strings.Cut(u.GitHubRepo, "/")
	return repo + "/" + u.Version
}
