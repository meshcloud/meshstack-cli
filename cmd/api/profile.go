package api

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"

	"github.com/meshcloud/meshstack-cli/client/types/xurl"
	"github.com/meshcloud/meshstack-cli/cmd/internal"
	"github.com/meshcloud/meshstack-cli/internal/meshstack"
	"github.com/meshcloud/meshstack-cli/internal/profile"
	"github.com/meshcloud/meshstack-cli/internal/setting"
)

// profileFor fails where a profile or endpoint named by flag or environment does not hold the URL,
// rather than send that profile's credential to another meshStack. The endpoint it returns is the
// one to cut the URL at, and is zero where the session is to create the profile named.
func profileFor(ctx context.Context, requestURL *url.URL) (endpoint xurl.URL, sources setting.Sources, err error) {
	settingSources := internal.SettingSources()
	profiles, err := profile.LoadProfiles(ctx, profile.LoadProfilesOptions{SettingSources: settingSources})
	if err != nil {
		return xurl.URL{}, nil, err
	}

	explicitEndpoint, endpointSource, err := settingSources.ResolveSettingWithSource(ctx, meshstack.EndpointSetting)
	endpointIsExplicit := err == nil
	if err != nil && !errors.Is(err, setting.ErrNoSourceProvidedValue) {
		return xurl.URL{}, nil, err
	}
	// Checked before the profile name: where both are named, the error is about the endpoint, as it
	// overrides the profile's.
	if endpointIsExplicit {
		if _, held := explicitEndpoint.PathTo(requestURL); !held {
			return xurl.URL{}, nil, notHeldError(profiles, requestURL, explicitEndpoint, "", endpointSource.Describe(meshstack.EndpointSetting.EnvKey()))
		}
	}

	name, nameSource, err := settingSources.ResolveSettingWithSource(ctx, profile.NameSetting)
	if err != nil {
		return xurl.URL{}, nil, err
	}
	if _, isDefault := nameSource.(setting.DefaultSource); !isDefault {
		named, stored := profiles.Profiles[name]
		if !stored {
			return explicitEndpoint, nil, nil
		}
		if _, held := named.Endpoint.PathTo(requestURL); !held {
			return xurl.URL{}, nil, notHeldError(profiles, requestURL, named.Endpoint, fmt.Sprintf(" of profile '%s'", named.Name),
				nameSource.Describe(profile.NameSetting.EnvKey()))
		}
		if endpointIsExplicit {
			return explicitEndpoint, nil, nil
		}
		return named.Endpoint, nil, nil
	}

	var selected profile.Profile
	if endpointIsExplicit {
		if len(profiles.Profiles) == 0 {
			return explicitEndpoint, nil, nil
		}
		if selected, _, err = profiles.ForEndpoint(explicitEndpoint, requestURL); err != nil {
			return xurl.URL{}, nil, err
		}
		endpoint = explicitEndpoint
	} else {
		var current bool
		if selected, current, err = profiles.Holding(requestURL); err != nil {
			return xurl.URL{}, nil, err
		}
		if !current {
			slog.WarnContext(ctx, fmt.Sprintf("Using profile '%s' for %s, as its endpoint %s holds it, rather than the current profile '%s'",
				selected, requestURL.Redacted(), selected.Endpoint, profiles.CurrentProfile))
		}
		endpoint = selected.Endpoint
	}
	return endpoint, setting.Sources{setting.FallbackSource{Source: setting.LookupSource{
		MatchingKey: profile.NameSetting.EnvKey(),
		Description: "profile whose endpoint holds " + requestURL.Redacted(),
		Func:        func(context.Context) (string, error) { return string(selected.Name), nil },
	}}}, nil
}

func notHeldError(profiles profile.Profiles, requestURL *url.URL, endpoint xurl.URL, ofProfile, source string) error {
	urlEndpoint, _ := profiles.EndpointHolding(requestURL)
	return fmt.Errorf("%s is for endpoint %s, not for endpoint %s%s from %s; leave out %s, or give the path relative to %s",
		requestURL.Redacted(), urlEndpoint, endpoint, ofProfile, source, source, endpoint)
}
