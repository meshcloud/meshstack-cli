package auth

import (
	"fmt"
	"slices"

	"github.com/meshcloud/meshstack-cli/client/types/enum"
	"github.com/meshcloud/meshstack-cli/internal/auth/credential"
)

// Method names one way of authenticating, as [ResolveSessionOptions.ForceAuthWith] takes it.
type Method = credential.Name

var (
	methods = enum.Enum[Method]{}

	// ApiKeyMethod mints a token from an API key id and secret.
	ApiKeyMethod = methods.Entry("apiKey").Unwrap()
	// ManualMethod sends an access token as it is.
	ManualMethod = methods.Entry("manual").Unwrap()
)

func init() {
	var ms []Method
	for _, method := range methods {
		ms = append(ms, method.Unwrap())
	}
	slices.Sort(ms)
	if slices.Compare(ms, credential.Names) != 0 {
		panic(fmt.Sprintf("method enums %s do not match credential names %s", ms, credential.Names))
	}
}
