package openapi

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"slices"
	"strings"
)

// apiKeyLoginPath is the token exchange of an API key, which internal/auth/credential sends for
// every command that needs a token.
const apiKeyLoginPath = "/api/login"

// withoutAuthentication leaves out what the document says about authentication, because the CLI
// authenticates every request itself, and much of it is outdated: the security requirements of the
// document and of each operation, the security schemes they name, the paragraphs of a description
// about them, and the operation of the API key login.
func withoutAuthentication(document jsontext.Value) (jsontext.Value, error) {
	var members object
	if err := json.Unmarshal(document, &members); err != nil {
		return nil, err
	}
	members = members.without("security")
	err := members.update("components", func(components object) (object, error) {
		return components.without("securitySchemes"), nil
	})
	if err == nil {
		err = members.update("paths", func(paths object) (object, error) {
			paths = paths.without(apiKeyLoginPath)
			return paths, paths.updateAll(func(path object) (object, error) {
				return path, path.updateAll(func(operation object) (object, error) {
					operation = operation.without("security")
					return operation, operation.updateString("description", withoutAuthenticationParagraphs)
				})
			})
		})
	}
	if err != nil {
		return nil, err
	}
	return json.Marshal(members)
}

// withoutAuthenticationParagraphs leaves out a paragraph such as "**Authentication:** This endpoint
// supports API Key authentication.", and one that asks for "Basic Authentication with an API User".
func withoutAuthenticationParagraphs(description string) string {
	paragraphs := slices.DeleteFunc(strings.Split(description, "\n\n"), func(paragraph string) bool {
		return strings.HasPrefix(strings.TrimSpace(paragraph), "**Authentication:**") ||
			strings.Contains(paragraph, "Basic Authentication")
	})
	return strings.TrimSpace(strings.Join(paragraphs, "\n\n"))
}
