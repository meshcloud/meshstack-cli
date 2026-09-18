package credential

import (
	"context"
	"fmt"
	"maps"
	"reflect"
	"slices"

	"github.com/meshcloud/meshstack-cli/internal/meshstack"
)

type Name string

func (n Name) String() string {
	return string(n)
}

const (
	// ApiKeyName mints a token from an API key id and secret.
	ApiKeyName Name = "apiKey"
	// ManualName sends an access token as it is.
	ManualName Name = "manual"
	// OidcLoginName logs a person in through a browser, so it resolves only when asked for by name.
	OidcLoginName Name = "oidcLogin"
)

// Names lists every credential, in the order a resolution tries and reports them.
var Names = []Name{ApiKeyName, ManualName, OidcLoginName}

type nameContextKey int

func NameFromContext(ctx context.Context) (Name, error) {
	name, found := ctx.Value(nameContextKey(0)).(Name)
	if !found {
		// An error rather than a panic, because a front end reaches this through pkg/auth and a
		// panic there takes its process down.
		return "", fmt.Errorf("no credential available in this context; it is only provided while resolving %s", meshstack.WorkspaceSetting.EnvKey())
	}
	return name, nil
}

func SetNameInContext(ctx context.Context, name Name) context.Context {
	return context.WithValue(ctx, nameContextKey(0), name)
}

// A name that no field of Credentials carries resolves to nothing and stores to nowhere, without
// saying so, which is why the two lists are checked against each other at startup.
func init() {
	fields := slices.Sorted(maps.Keys(maps.Collect((&Credentials{}).fields())))
	if !slices.Equal(fields, slices.Sorted(slices.Values(Names))) {
		panic(fmt.Sprintf("credential names %v do not match the fields of Credentials %v", Names, fields))
	}
}

func (cs *Credentials) NameOf(credential Credential) (out Name) {
	cs.withFieldFor(credential, func(name Name, _ reflect.Value) {
		out = name
	})
	return
}

func (cs *Credentials) ByName(name Name) Credential {
	for candidate, field := range cs.fields() {
		if candidate == name && !field.IsNil() {
			return field.Interface().(Credential) //nolint:forcetypeassert // a pointer field that is not a Credential is a mistake in the struct above
		}
	}
	return nil
}
