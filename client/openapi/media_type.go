package openapi

import (
	"bytes"
	"cmp"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// Only a hal+json type carries a meshObject: application/vnd.meshcloud.api.meshobjects.v1+json is
// a collection of them.
var versionedMediaTypeRe = regexp.MustCompile(`^application/vnd\.meshcloud\.api\.([a-z]+)\.(v\d+(?:-preview)?)(\.hal)?\+json$`)

type MediaType struct {
	Name string
	// Kind is empty for a type of no meshObject.
	Kind string
	// ApiVersion is zero for a type of no version, such as application/json.
	ApiVersion ApiVersion
}

// newMediaType takes the camel case of the kind from the operationId, which starts with the kind in
// every operation of the meshObject API, as meshBuildingBlockPost does.
func newMediaType(name, operationId string) MediaType {
	match := versionedMediaTypeRe.FindStringSubmatch(name)
	if match == nil {
		return MediaType{Name: name}
	}
	mediaType := MediaType{Name: name, ApiVersion: MustParseApiVersion(match[2])}
	if kind, isMeshObject := match[1], match[3] != ""; isMeshObject {
		mediaType.Kind = kind
		if prefix := operationId[:min(len(operationId), len(kind))]; strings.EqualFold(prefix, kind) {
			mediaType.Kind = prefix
		}
	}
	return mediaType
}

// ParseMediaType reads a media type such as an Accept header carries. Its Kind is in lower case,
// as the media type writes it.
func ParseMediaType(name string) MediaType {
	return newMediaType(name, "")
}

// WithKindAndApiVersion adds the kind and apiVersion a meshObject body lacks, in front, as the
// meshObject API writes them.
func (m MediaType) WithKindAndApiVersion(body jsontext.Value) (jsontext.Value, error) {
	if m.Kind == "" || body.Kind() != '{' {
		return body, nil
	}
	var members object
	if err := json.Unmarshal(body, &members); err != nil {
		return nil, err
	}
	var missing object
	for _, field := range []member{
		{"apiVersion", jsontext.Value(strconv.Quote(m.ApiVersion.String()))},
		{"kind", jsontext.Value(strconv.Quote(m.Kind))},
	} {
		value, ok := members.get(field.name)
		if !ok {
			missing = append(missing, field)
		} else if !bytes.Equal(value, field.value) {
			return nil, fmt.Errorf("the body's %s is %s, but %s takes %s", field.name, value, m.Name, field.value)
		}
	}
	return json.Marshal(append(missing, members...))
}

type ApiVersion struct {
	number  int
	preview bool
}

func ParseApiVersion(s string) (ApiVersion, error) {
	rest, hasPrefix := strings.CutPrefix(s, "v")
	digits, preview := strings.CutSuffix(rest, "-preview")
	number, err := strconv.Atoi(digits)
	if !hasPrefix || err != nil || number < 1 {
		return ApiVersion{}, fmt.Errorf("%q is no API version, such as v1 or v2-preview", s)
	}
	return ApiVersion{number, preview}, nil
}

func MustParseApiVersion(s string) ApiVersion {
	version, err := ParseApiVersion(s)
	if err != nil {
		panic(err)
	}
	return version
}

func (v ApiVersion) IsZero() bool {
	return v == ApiVersion{}
}

// String is empty for the zero version.
func (v ApiVersion) String() string {
	switch {
	case v.IsZero():
		return ""
	case v.preview:
		return fmt.Sprintf("v%d-preview", v.number)
	default:
		return fmt.Sprintf("v%d", v.number)
	}
}

func (v ApiVersion) Compare(other ApiVersion) int {
	if c := cmp.Compare(v.number, other.number); c != 0 || v.preview == other.preview {
		return c
	}
	if v.preview {
		return -1
	}
	return 1
}

type ApiVersions []ApiVersion

// SortedUnique sorts in place, oldest first.
func (vs ApiVersions) SortedUnique() ApiVersions {
	slices.SortFunc(vs, ApiVersion.Compare)
	return slices.Compact(vs)
}

func (vs ApiVersions) String() string {
	names := make([]string, len(vs))
	for i, version := range vs {
		names[i] = version.String()
	}
	return strings.Join(names, ", ")
}
