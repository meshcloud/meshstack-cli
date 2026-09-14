package credential

import "reflect"

type Name string

var Names = func() (result []Name) {
	for name := range (&Credentials{}).fields() {
		result = append(result, name)
	}
	return
}()

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
			return field.Interface().(Credential) //nolint:forcetypeassert
		}
	}
	return nil
}
