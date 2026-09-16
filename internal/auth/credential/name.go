package credential

import (
	"maps"
	"reflect"
	"slices"
)

type Name string

func (n Name) String() string {
	return string(n)
}

var Names = slices.Sorted(maps.Keys(maps.Collect((&Credentials{}).fields())))

func (cs *Credentials) NameOf(credential Credential) (out Name) {
	cs.withFieldFor(credential, func(name Name, _ reflect.Value) {
		out = name
	})
	return
}

func (cs *Credentials) ByName(name Name) Credential {
	for candidate, field := range cs.fields() {
		if candidate == name && !field.IsNil() {
			// A pointer field that is not a Credential is a mistake in the struct above
			return field.Interface().(Credential) //nolint:forcetypeassert // a pointer field that is not a Credential is a mistake in the struct above
		}
	}
	return nil
}
