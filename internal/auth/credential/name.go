package credential

import (
	"fmt"
	"maps"
	"slices"
)

type Name string

func (n Name) String() string {
	return string(n)
}

const (
	ApiKeyName    Name = "apiKey"
	ManualName    Name = "manual"
	OidcLoginName Name = "oidcLogin"
)

// Names lists every credential, in the order a resolution tries and reports them.
var Names = []Name{ApiKeyName, ManualName, OidcLoginName}

// A name that no field of Credentials carries resolves to nothing and stores to nowhere, without
// saying so, which is why the two lists are checked against each other at startup.
func init() {
	fields := slices.Sorted(maps.Keys(maps.Collect((&Credentials{}).fields())))
	if !slices.Equal(fields, slices.Sorted(slices.Values(Names))) {
		panic(fmt.Sprintf("credential names %v do not match the fields of Credentials %v", Names, fields))
	}
}

func (cs *Credentials) ByName(name Name) Credential {
	for candidate, field := range cs.fields() {
		if candidate == name && !field.IsNil() {
			return field.Interface().(Credential) //nolint:forcetypeassert // a pointer field that is not a Credential is a mistake in the struct above
		}
	}
	return nil
}
