package auth

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/meshcloud/meshstack-cli/client/types/xurl"
	"github.com/meshcloud/meshstack-cli/pkg/credential"
	"github.com/meshcloud/meshstack-cli/pkg/diags"
	"github.com/meshcloud/meshstack-cli/pkg/meshstack"
	"github.com/meshcloud/meshstack-cli/pkg/oidc/jwt"
	"github.com/meshcloud/meshstack-cli/pkg/profile"
	"github.com/meshcloud/meshstack-cli/pkg/setting"
)

type resolvedCredential struct {
	credential credential.Credential
	// fromProfile decides the store, and with it whether anything this session mints can ever
	// reach a file.
	fromProfile bool
	origins     []setting.Origin
}

// offer is what one source above the profile carries. A source offering nothing is left out.
type offer struct {
	from   setting.Source
	id     string
	token  string
	secret string
}

// resolveCredential walks the ranked sources once. The first one carrying an identity defines
// the credential; its secret is its own secret slot when it has one, otherwise the first
// secret offered by a source that carries no competing identity.
//
// It is not setting.Resolve, which resolves one value where this rule relates three. It has to
// be one walk because the alternative is pairing an id from one place with a secret from
// another, which meshStack answers with a 401 that names neither of them.
func resolveCredential(ctx context.Context, opts ResolveSessionOptions, selection profile.Selection, endpoint xurl.URL, sources []setting.Source) (resolvedCredential, error) {
	above, err := offers(sources)
	if err != nil {
		return resolvedCredential{}, err
	}

	// stored opens credentials/<profile>.json on first use and not before. A run whose
	// credential came from the environment must not fail because the default profile on this
	// machine points at another meshStack, and that check lives behind this read.
	var read *profile.Credentials
	stored := func() (profile.Credentials, error) {
		if read != nil {
			return *read, nil
		}
		store := opts.Store
		if store == nil {
			opened, err := profile.NewFileStore(selection.Name)
			if err != nil {
				return profile.Credentials{}, err
			}
			store = opened
		}
		credentials, err := store.Read()
		if err != nil {
			return profile.Credentials{}, err
		}
		// Whole-file rather than per-method, because the common path uses a cached access
		// token without consulting a method at all: without this, repointing a profile's
		// endpoint would send a stored bearer token to a different meshStack.
		if credentials.Endpoint != nil && !meshstack.SameEndpoint(*credentials.Endpoint, endpoint) {
			return profile.Credentials{}, diags.Errorf("this credential belongs to a different meshStack",
				"profile %q was logged in to %s, but this command targets %s. Name another profile with %s, or log in again.",
				selection.Name, credentials.Endpoint, endpoint, profile.Name.EnvKey)
		}
		read = &credentials
		return credentials, nil
	}

	wants := func(method credential.Method) bool {
		return opts.DemandMethod == "" || opts.DemandMethod == method
	}
	for _, carrying := range above {
		switch {
		case carrying.token != "" && wants(credential.MethodManual):
			token, err := jwt.Parse(carrying.token)
			if err != nil {
				return resolvedCredential{}, diags.Wrap(err, "this is not a meshStack API token",
					"what %s supplied could not be read as an access token: %v",
					carrying.from.Describe(credential.ApiToken.EnvKey), err)
			}
			return resolvedCredential{
				credential: credential.FromManual(credential.Manual{AccessToken: issuedToken(token)}),
				origins: []setting.Origin{{Key: credential.ApiToken.EnvKey,
					Source: carrying.from.Describe(credential.ApiToken.EnvKey)}},
			}, nil
		case carrying.id != "" && wants(credential.MethodApiKey):
			return apiKeyAbove(ctx, above, carrying, selection.Name, stored)
		}
	}
	return fromTheProfile(ctx, opts.DemandMethod, above, selection.Name, stored)
}

func offers(sources []setting.Source) ([]offer, error) {
	found := make([]offer, 0, len(sources))
	for _, source := range sources {
		if source == nil {
			continue
		}
		carrying := offer{
			from:   source,
			id:     text(source, credential.ApiKeyId),
			token:  text(source, credential.ApiToken),
			secret: text(source, credential.ApiSecret),
		}
		if carrying.id != "" && carrying.token != "" {
			// Two methods rather than two spellings of one thing, so picking one silently
			// would hand the user an identity they did not choose.
			return nil, diags.Errorf("two authentication methods from one place",
				"%s and %s are both set. They are different methods, and a token needs no key; remove one.",
				source.Describe(credential.ApiKeyId.EnvKey), source.Describe(credential.ApiToken.EnvKey))
		}
		if carrying.id != "" || carrying.token != "" || carrying.secret != "" {
			found = append(found, carrying)
		}
	}
	return found, nil
}

// apiKeyAbove completes an API key whose id came from above the profile. That is the normal
// non-interactive setup — an id in the provider block or on --api-key, the secret in the
// environment — rather than an edge case.
func apiKeyAbove(ctx context.Context, above []offer, winner offer, name string, stored func() (profile.Credentials, error)) (resolvedCredential, error) {
	key := credential.ApiKey{Id: winner.id}
	origins := []setting.Origin{{
		Key: credential.ApiKeyId.EnvKey, Source: winner.from.Describe(credential.ApiKeyId.EnvKey),
	}}
	secretFrom := ""
	competing := ""

	for _, carrying := range above {
		switch {
		case carrying.secret == "":
		case carrying.id != "" && carrying.id != winner.id:
			// A secret sitting beside another id belongs to that id. Pairing it with this
			// one produces a 401 that names neither.
			if competing == "" {
				competing = carrying.id
			}
		case key.Secret == "":
			key.Secret, secretFrom = carrying.secret, carrying.from.Describe(credential.ApiSecret.EnvKey)
		case carrying.secret != key.Secret:
			slog.WarnContext(ctx, "another API key secret is set and ignored", "detail", fmt.Sprintf(
				"%s is also set; the secret from %s is the one being used.",
				carrying.from.Describe(credential.ApiSecret.EnvKey), secretFrom))
		}
	}

	// The profile is the bottom of the same list, which is what lets an id exported for this
	// run pair with a clientSecretCommand the profile already knows how to run.
	if key.Secret == "" {
		credentials, err := stored()
		if err != nil {
			return resolvedCredential{}, err
		}
		if held := credentials.ApiKey; held != nil && held.Id == winner.id && hasSecret(*held) {
			key, secretFrom = *held, "profile "+name
		}
	}
	if !hasSecret(key) {
		return resolvedCredential{}, missingSecret(winner.from.Describe(credential.ApiKeyId.EnvKey), key.Id, competing)
	}
	origins = append(origins, setting.Origin{Key: credential.ApiSecret.EnvKey, Source: secretFrom})
	return resolvedCredential{credential: credential.FromApiKey(key), origins: origins}, nil
}

// fromTheProfile is the bottom of the ranked order. The credential it returns keeps every
// method the file holds, so that `meshstack login` switches back to a stored browser login
// without a new browser session; Current is the one this run authenticates with.
func fromTheProfile(ctx context.Context, demanded credential.Method, above []offer, name string, stored func() (profile.Credentials, error)) (resolvedCredential, error) {
	credentials, err := stored()
	if err != nil {
		return resolvedCredential{}, err
	}
	current := credentials.Current
	if demanded != "" {
		current = demanded
	}
	if current == "" {
		// Nothing stored yet. Login is the method `meshstack login` creates, and the error a
		// command gets from a profile with no credentials names it.
		current = credential.MethodLogin
	}

	resolved := resolvedCredential{credential: credentials.Credential, fromProfile: true}
	resolved.credential.Current = current
	switch current {
	case credential.MethodApiKey:
		if credentials.ApiKey == nil || credentials.ApiKey.Id == "" {
			// Only a login can demand this method without an id, and its own exchange is
			// where the message naming --api-key belongs.
			return resolved, nil
		}
		held := *credentials.ApiKey
		secretFrom := "profile " + name
		switch {
		case hasSecret(held):
			warnStoredSecretWins(ctx, above, held, name)
		default:
			for _, carrying := range above {
				if carrying.secret == "" || (carrying.id != "" && carrying.id != held.Id) {
					continue
				}
				held.Secret, secretFrom = carrying.secret, carrying.from.Describe(credential.ApiSecret.EnvKey)
				break
			}
		}
		if !hasSecret(held) {
			return resolvedCredential{}, missingSecret("profile "+name, held.Id, "")
		}
		resolved.credential.ApiKey = &held
		resolved.origins = []setting.Origin{
			{Key: credential.ApiKeyId.EnvKey, Source: "profile " + name},
			{Key: credential.ApiSecret.EnvKey, Source: secretFrom},
		}
	case credential.MethodManual:
		if demanded == credential.MethodManual {
			// A token cannot be renewed, so demanding this method is asking for a new one and
			// the profile's stored token is not an answer to it.
			return resolvedCredential{}, diags.Wrap(ErrNoApiToken, "no meshStack API token",
				"set %s, or pipe one in with `meshstack login --api-token --api-token-stdin`. A token is never a flag value, because a flag value lands in shell history, in ps output and in CI logs.",
				credential.ApiToken.EnvKey)
		}
		resolved.origins = []setting.Origin{{Key: credential.ApiToken.EnvKey, Source: "profile " + name}}
	}
	return resolved, nil
}

// warnStoredSecretWins is the cost of keeping the profile's paired secret: a rotated
// MESHSTACK_API_SECRET beside a profile still serving the old one looks exactly like a
// revoked key. Storing the environment's secret instead is rejected — which of the two is
// newer is not knowable, and it would make every read path take the write lock.
func warnStoredSecretWins(ctx context.Context, above []offer, held credential.ApiKey, name string) {
	if held.Secret == "" {
		// Comparing against a clientSecretCommand would mean running it during a resolution.
		return
	}
	for _, carrying := range above {
		if carrying.secret == "" || carrying.secret == held.Secret {
			continue
		}
		slog.WarnContext(ctx, "the stored API key secret is being used", "detail", fmt.Sprintf(
			"%s is set and differs from the secret stored for %s in profile %q. The stored one is being used. To replace it, run `meshstack login --api-key=%s --api-secret-stdin`.",
			carrying.from.Describe(credential.ApiSecret.EnvKey), held.Id, name, held.Id))
		return
	}
}

// missingSecret quotes the losing id deliberately: a client id is not a secret, and it is
// the fact that identifies which stale export to remove.
func missingSecret(where, id, competing string) error {
	if competing == "" {
		return diags.Wrap(ErrNoApiSecret, "no API key secret",
			"%s names the API key %s, and nothing supplies its secret. Set %s, or run `meshstack login --api-key=%s --api-secret-stdin`.",
			where, id, credential.ApiSecret.EnvKey, id)
	}
	return diags.Wrap(ErrNoApiSecret, "no API key secret",
		"%s names the API key %s, and no secret is available for it. %s is set, but it belongs to %s (%s), which %s overrides. Supply the secret beside %s, or unset %s so that %s pairs with it.",
		where, id, credential.ApiSecret.EnvKey, credential.ApiKeyId.EnvKey, competing, where,
		where, credential.ApiKeyId.EnvKey, credential.ApiSecret.EnvKey)
}

func hasSecret(key credential.ApiKey) bool {
	return key.Secret != "" || len(key.SecretCommand) > 0
}

func text(source setting.Source, of setting.Value[string]) string {
	value, ok := source.Lookup(of.EnvKey)
	if !ok {
		return ""
	}
	return strings.TrimSpace(value)
}
