// Package openapi reads the OpenAPI document meshStack publishes, and picks the operations of a
// request path, a method and a version out of it. It does not know where the document comes from.
package openapi

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"fmt"
	"io"
	"maps"
	"regexp"
	"slices"
	"strings"
)

// Spec is an OpenAPI document. After Select, it marshals to the selected operations and the
// components they reference.
type Spec struct {
	// document is nil after Select.
	document   jsontext.Value
	Operations []Operation
	components []component
}

type Operation struct {
	Method       string
	PathTemplate PathTemplate
	// Kind is the meshObject kind below whose path the operation is, such as meshBuildingBlock, or
	// empty outside the path of a kind.
	Kind string
	// Action names the operation within its kind, as a command of the CLI would: list, show,
	// create, update, delete, or the path below the kind's object, such as trigger-run.
	Action string
	// MediaTypes are those of the request body and of every response, each once.
	MediaTypes []MediaType

	raw jsontext.Value
}

type component struct {
	member

	kind string
}

func (c component) ref() string {
	return "#/components/" + c.kind + "/" + c.name
}

// Parse leaves out what the document says about authentication, see withoutAuthentication.
func Parse(r io.Reader) (Spec, error) {
	read, err := io.ReadAll(r)
	if err != nil {
		return Spec{}, err
	}
	document, err := withoutAuthentication(read)
	if err != nil {
		return Spec{}, fmt.Errorf("cannot parse the OpenAPI document: %w", err)
	}
	var parsed struct {
		Paths      object            `json:"paths"`
		Components map[string]object `json:"components"`
	}
	if err := json.Unmarshal(document, &parsed); err != nil {
		return Spec{}, fmt.Errorf("cannot parse the OpenAPI document: %w", err)
	}
	spec := Spec{document: document}
	for _, path := range parsed.Paths {
		var methods object
		if err := json.Unmarshal(path.value, &methods); err != nil {
			return Spec{}, fmt.Errorf("cannot parse path %s: %w", path.name, err)
		}
		for _, method := range methods {
			operation, err := parseOperation(strings.ToUpper(method.name), path.name, method.value)
			if err != nil {
				return Spec{}, fmt.Errorf("cannot parse %s %s: %w", method.name, path.name, err)
			}
			spec.Operations = append(spec.Operations, operation)
		}
	}
	spec.assignKinds()
	for _, kind := range slices.Sorted(maps.Keys(parsed.Components)) {
		for _, c := range parsed.Components[kind] {
			spec.components = append(spec.components, component{kind: kind, member: c})
		}
	}
	return spec, nil
}

func parseOperation(method, pathTemplate string, raw jsontext.Value) (Operation, error) {
	type content struct {
		Content map[string]jsontext.Value `json:"content"`
	}
	var parsed struct {
		OperationId string             `json:"operationId"`
		RequestBody content            `json:"requestBody"`
		Responses   map[string]content `json:"responses"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return Operation{}, err
	}
	names := slices.Collect(maps.Keys(parsed.RequestBody.Content))
	for _, response := range parsed.Responses {
		names = slices.AppendSeq(names, maps.Keys(response.Content))
	}
	slices.Sort(names)
	operation := Operation{Method: method, PathTemplate: PathTemplate(pathTemplate), raw: raw}
	for _, name := range slices.Compact(names) {
		operation.MediaTypes = append(operation.MediaTypes, newMediaType(name, parsed.OperationId))
	}
	return operation, nil
}

// ApiVersions are oldest first.
func (o Operation) ApiVersions() ApiVersions {
	var versions ApiVersions
	for _, mediaType := range o.MediaTypes {
		if !mediaType.ApiVersion.IsZero() {
			versions = append(versions, mediaType.ApiVersion)
		}
	}
	return versions.SortedUnique()
}

// LatestApiVersion is zero for an operation of no version.
func (o Operation) LatestApiVersion() ApiVersion {
	versions := o.ApiVersions()
	if len(versions) == 0 {
		return ApiVersion{}
	}
	return versions[len(versions)-1]
}

// LatestMediaType is the one of the latest version, or for an operation of no version the first,
// such as application/json. It is the one to accept, and to send a body in.
func (o Operation) LatestMediaType() (MediaType, bool) {
	if mediaType, ok := o.MediaType(o.LatestApiVersion()); ok {
		return mediaType, true
	}
	if len(o.MediaTypes) == 0 {
		return MediaType{}, false
	}
	return o.MediaTypes[0], true
}

func (o Operation) MediaType(version ApiVersion) (MediaType, bool) {
	for _, mediaType := range o.MediaTypes {
		if mediaType.ApiVersion == version {
			return mediaType, true
		}
	}
	return MediaType{}, false
}

type Selector struct {
	// Kind and Action select by Operation.Kind and Operation.Action. Kind is matched in any case.
	Kind   string
	Action string
	// Method is empty for every method.
	Method string
	// Path is a request path such as /api/meshobjects/meshtenants/<uuid>, or empty for every path.
	Path string
	// ApiVersion is zero for the latest version each operation offers.
	ApiVersion ApiVersion
}

func (s Selector) IsZero() bool {
	return s == Selector{}
}

func (s Selector) matches(operation Operation) bool {
	return (s.Method == "" || strings.EqualFold(operation.Method, s.Method)) &&
		(s.Path == "" || operation.PathTemplate.matches(s.Path)) &&
		(s.Kind == "" || strings.EqualFold(operation.Kind, s.Kind)) &&
		(s.Action == "" || operation.Action == s.Action)
}

// Select keeps the operations the selector matches, and of their media types those of one version.
func (s Spec) Select(selector Selector) (Spec, error) {
	if selector.IsZero() {
		return s, nil
	}
	matched := slices.DeleteFunc(slices.Clone(s.Operations), func(operation Operation) bool {
		return !selector.matches(operation)
	})
	// The templates of one method that match a path, or of one action of a kind, are versions of one
	// operation, because meshStack names a path parameter differently in different versions:
	// meshtenants/{tenantIdentifier} in v3 and meshtenants/{uuid} in v4.
	operationKey := func(operation Operation) string {
		switch {
		case selector.Path != "":
			return operation.Method
		case operation.Kind != "":
			return operation.Kind + " " + operation.Method + " " + operation.Action
		default:
			return operation.Method + " " + string(operation.PathTemplate)
		}
	}

	selected := Spec{components: s.components}
	for _, operations := range groupBy(matched, operationKey) {
		operations = mostLiteral(operations)
		var offered ApiVersions
		for _, operation := range operations {
			offered = append(offered, operation.ApiVersions()...)
		}
		offered = offered.SortedUnique()
		version := selector.ApiVersion
		if len(offered) > 0 {
			if version.IsZero() {
				version = offered[len(offered)-1]
			}
			if !slices.Contains(offered, version) {
				if selector.Path == "" {
					continue
				}
				return Spec{}, fmt.Errorf("%s %s offers %s, not %s", operations[0].Method, selector.Path, offered, version)
			}
			operations = slices.DeleteFunc(operations, func(operation Operation) bool {
				return !slices.Contains(operation.ApiVersions(), version)
			})
		}
		for _, operation := range operations {
			narrowed, err := operation.withVersion(version)
			if err != nil {
				return Spec{}, err
			}
			selected.Operations = append(selected.Operations, narrowed)
		}
	}
	return selected, nil
}

// mostLiteral keeps the templates that have a literal segment where the others have a parameter.
func mostLiteral(operations []Operation) []Operation {
	// The templates all match one path, so they agree on every literal segment, and a parameter
	// written as "" sorts below it.
	literals := func(operation Operation) []segment {
		literal := operation.PathTemplate.segments()
		for i, segment := range literal {
			if segment.isParameter() {
				literal[i] = ""
			}
		}
		return literal
	}
	compare := func(a, b Operation) int {
		return slices.Compare(literals(a), literals(b))
	}
	most := slices.MaxFunc(operations, compare)
	return slices.DeleteFunc(operations, func(operation Operation) bool {
		return compare(operation, most) < 0
	})
}

func (o Operation) withVersion(version ApiVersion) (Operation, error) {
	if version.IsZero() {
		return o, nil
	}
	keep := func(name string) bool {
		mediaTypeVersion := ParseMediaType(name).ApiVersion
		return mediaTypeVersion.IsZero() || mediaTypeVersion == version
	}
	o.MediaTypes = slices.DeleteFunc(slices.Clone(o.MediaTypes), func(mediaType MediaType) bool {
		return !keep(mediaType.Name)
	})
	for _, content := range [][]string{{"requestBody", "content"}, {"responses", "*", "content"}} {
		var err error
		if o.raw, err = keepMembers(o.raw, content, keep); err != nil {
			return o, err
		}
	}
	return o, nil
}

// keepMembers deletes the members that keep rejects from the object at path, in which "*" stands
// for every member.
func keepMembers(value jsontext.Value, path []string, keep func(name string) bool) (jsontext.Value, error) {
	var members object
	if err := json.Unmarshal(value, &members); err != nil {
		return nil, err
	}
	if len(path) == 0 {
		return json.Marshal(slices.DeleteFunc(members, func(m member) bool { return !keep(m.name) }))
	}
	for i, m := range members {
		if path[0] == "*" || path[0] == m.name {
			var err error
			if members[i].value, err = keepMembers(m.value, path[1:], keep); err != nil {
				return nil, err
			}
		}
	}
	return json.Marshal(members)
}

func (s Spec) MarshalJSONTo(enc *jsontext.Encoder) error {
	if s.document != nil {
		return enc.WriteValue(s.document)
	}
	paths := nestedObject(s.Operations,
		func(operation Operation) string { return string(operation.PathTemplate) },
		func(operation Operation) member { return member{strings.ToLower(operation.Method), operation.raw} }).mustMarshal()
	document := object{{"paths", paths}}
	if referenced := s.referencedComponents(paths); len(referenced) > 0 {
		components := nestedObject(referenced,
			func(c component) string { return c.kind },
			func(c component) member { return c.member })
		document = append(document, member{"components", components.mustMarshal()})
	}
	return json.MarshalEncode(enc, document)
}

var refRe = regexp.MustCompile(`"\$ref"\s*:\s*"([^"]+)"`)

func (s Spec) referencedComponents(value jsontext.Value) []component {
	byRef := map[string]component{}
	for _, c := range s.components {
		byRef[c.ref()] = c
	}
	referenced := map[string]bool{}
	var follow func(value jsontext.Value)
	follow = func(value jsontext.Value) {
		for _, match := range refRe.FindAllSubmatch(value, -1) {
			ref := string(match[1])
			if c, ok := byRef[ref]; ok && !referenced[ref] {
				referenced[ref] = true
				follow(c.value)
			}
		}
	}
	follow(value)
	return slices.DeleteFunc(slices.Clone(s.components), func(c component) bool {
		return !referenced[c.ref()]
	})
}

// groupBy keeps the order in which each key first appears.
func groupBy[V any, K comparable](values []V, key func(V) K) [][]V {
	var groups [][]V
	indexOf := map[K]int{}
	for _, value := range values {
		i, ok := indexOf[key(value)]
		if !ok {
			i = len(groups)
			indexOf[key(value)] = i
			groups = append(groups, nil)
		}
		groups[i] = append(groups[i], value)
	}
	return groups
}

// nestedObject groups the values into an object of objects: {outer: {inner.name: inner.value}}.
func nestedObject[V any](values []V, outer func(V) string, inner func(V) member) object {
	var nested object
	for _, group := range groupBy(values, outer) {
		var members object
		for _, value := range group {
			members = append(members, inner(value))
		}
		nested = append(nested, member{outer(group[0]), members.mustMarshal()})
	}
	return nested
}

func (o object) mustMarshal() jsontext.Value {
	value, err := json.Marshal(o)
	if err != nil {
		panic(err)
	}
	return value
}
