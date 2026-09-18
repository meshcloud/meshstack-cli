package credential

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/v2"
	"fmt"
	"reflect"
)

type Identity struct {
	Hash string
}

// identityOf hashes the credential with its cache cleared, over a copy of the struct. Only Cache
// may be a pointer, which WithCacheOf asserts, so the copy shares nothing with the original.
func identityOf(credential Credential) Identity {
	identity := reflect.New(reflect.ValueOf(credential).Elem().Type())
	identity.Elem().Set(reflect.ValueOf(credential).Elem())
	clearCache(identity.Interface().(Credential)) //nolint:forcetypeassert // identity is a fresh copy of a Credential, so it is one
	marshaled, err := json.Marshal(identity.Interface())
	if err != nil {
		panic(fmt.Sprintf("cannot hash the identity of %T: %s", credential, err.Error()))
	}
	sum := sha256.Sum256(marshaled)
	return Identity{Hash: hex.EncodeToString(sum[:])}
}
