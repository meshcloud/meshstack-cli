package credential

import (
	"fmt"
	"reflect"
)

// WithCacheOf reaches the optional Cache field of a credential struct, such as ApiKey.
func WithCacheOf(credential Credential, action func(cache reflect.Value)) {
	for field, v := range reflect.ValueOf(credential).Elem().Fields() {
		if field.Name == "Cache" {
			if v.Kind() != reflect.Pointer {
				panic(fmt.Sprintf("credential Cache field must be a pointer in %T", credential))
			}
			if getJsonKey(field) != "-" {
				panic(fmt.Sprintf("credential Cache field must be json-ignored with '-' in %T", credential))
			}
			action(v)
		} else if v.Kind() == reflect.Pointer {
			// A second pointer would be shared rather than copied by identityOf.
			panic(fmt.Sprintf("credential field %s must NOT be a pointer in %T", field.Name, credential))
		}
	}
}

// newCache allocates the Cache field of a credential struct, whose type is anonymous and so
// cannot be written at a call site. Call it only once there is something to cache: a nil Cache
// is what tells CachedToken that nothing is cached.
func newCache(credential Credential) {
	WithCacheOf(credential, func(cache reflect.Value) {
		cache.Set(reflect.New(cache.Type().Elem()))
	})
}

func clearCache(credential Credential) {
	WithCacheOf(credential, func(cache reflect.Value) {
		cache.Set(reflect.Zero(cache.Type()))
	})
}
