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

// identityOf is essentially the credential with cache set to nil, so we deep-copy the
// struct (assuming cred structs have only the Cache as ptr field, which is asserted in WithCacheOf).
// This is used to implement Credential.Identity.
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
