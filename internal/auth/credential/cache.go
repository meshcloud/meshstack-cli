package credential

import (
	"fmt"
	"reflect"
)

// WithCacheOf allows access to the (optional) Cache field of a credential struct, such as ApiKey.
// It also validates the struct so that identityOf can copy it: Cache is a json-ignored pointer,
// and no other field is a pointer at all.
func WithCacheOf(credential Credential, action func(cache reflect.Value)) (ok bool) {
	for field, v := range reflect.ValueOf(credential).Elem().Fields() {
		// This is a convention for all structs implementing Credential:
		// Naming the field "Cache" (and embedding as many fields as you'd like)
		if field.Name == "Cache" {
			if v.Kind() != reflect.Pointer {
				panic(fmt.Sprintf("credential Cache field must be a pointer in %T", credential))
			}
			if getJsonKey(field) != "-" {
				panic(fmt.Sprintf("credential Cache field must be json-ignored with '-' in %T", credential))
			}
			action(v)
			ok = true
		} else if v.Kind() == reflect.Pointer {
			// otherwise, our identity copy does not work in adoptCacheIfIdentity matches
			panic(fmt.Sprintf("credential field %s must NOT be a pointer in %T", field.Name, credential))
		}
	}
	return
}

// newCache allocates the Cache field of a credential struct, whose type is anonymous and so
// cannot be written at a call site. Call it only once there is something to cache: a nil
// Cache is what tells CachedToken that nothing is cached, so allocating one any earlier makes
// an empty cache look like a full one.
func newCache(credential Credential) {
	WithCacheOf(credential, func(cache reflect.Value) {
		cache.Set(reflect.New(cache.Type().Elem()))
	})
}

func adoptCacheIfIdentityMatches(fromValue reflect.Value, to Credential) {
	if fromValue.IsNil() {
		return
	}
	from := fromValue.Interface().(Credential) //nolint:forcetypeassert
	if identityOf(from).Hash != identityOf(to).Hash {
		return
	}
	WithCacheOf(from, func(fromCache reflect.Value) {
		WithCacheOf(to, func(toCache reflect.Value) {
			toCache.Set(fromCache)
		})
	})
}

func clearCache(credential Credential) {
	WithCacheOf(credential, func(cache reflect.Value) {
		cache.Set(reflect.Zero(cache.Type()))
	})
}
