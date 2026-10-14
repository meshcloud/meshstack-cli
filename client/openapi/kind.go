package openapi

import (
	gohttp "net/http"
	"slices"
	"strings"
)

// Kinds are the meshObject kinds of the operations, such as meshBuildingBlock, sorted.
func (s Spec) Kinds() []string {
	var kinds []string
	for _, operation := range s.Operations {
		if operation.Kind != "" {
			kinds = append(kinds, operation.Kind)
		}
	}
	slices.Sort(kinds)
	return slices.Compact(kinds)
}

// Actions are those of the operations of one kind, in the order of the document.
func (s Spec) Actions(kind string) []string {
	var actions []string
	for _, operation := range s.Operations {
		if strings.EqualFold(operation.Kind, kind) && !slices.Contains(actions, operation.Action) {
			actions = append(actions, operation.Action)
		}
	}
	return actions
}

var actionOfMethod = map[string]string{
	gohttp.MethodPost:   "create",
	gohttp.MethodPut:    "update",
	gohttp.MethodPatch:  "update",
	gohttp.MethodDelete: "delete",
}

// assignKinds sets Kind and Action. The path of a kind is the literal start of the shortest
// template whose media types carry the kind, because every operation of a kind is below that
// path: POST .../meshbuildingblockruns/create as well as GET .../meshbuildingblockruns/{uuid}/logs.
// An operation without media types of its own, such as a DELETE, belongs to the kind of the
// longest path its template starts with.
func (s Spec) assignKinds() {
	operations := s.Operations
	type kind struct {
		name string
		path []segment
	}
	// The key is in lower case, because a media type writes the kind in lower case where its
	// operationId does not start with the kind.
	kinds := map[string]*kind{}
	for _, operation := range operations {
		path := operation.PathTemplate.literalStart()
		for _, mediaType := range operation.MediaTypes {
			if mediaType.Kind == "" {
				continue
			}
			key := strings.ToLower(mediaType.Kind)
			k, ok := kinds[key]
			if !ok {
				k = &kind{name: mediaType.Kind, path: path}
				kinds[key] = k
			}
			if len(path) < len(k.path) {
				k.path = path
			}
			if k.name == key {
				k.name = mediaType.Kind
			}
		}
	}
	// A kind whose path is the start of another kind's path, such as meshObjects at /api/meshobjects,
	// lists the other kinds rather than objects of its own.
	for key, k := range kinds {
		for _, other := range kinds {
			if other != k && startsWith(other.path, k.path) {
				delete(kinds, key)
				break
			}
		}
	}

	byKind := map[string][]int{}
	for i, operation := range operations {
		segments := operation.PathTemplate.segments()
		var owner *kind
		for _, k := range kinds {
			if startsWith(segments, k.path) && (owner == nil || len(k.path) > len(owner.path)) {
				owner = k
			}
		}
		if owner == nil {
			continue
		}
		operations[i].Kind = owner.name
		operations[i].Action = action(operation.Method, segments[len(owner.path):])
		byKind[owner.name] = append(byKind[owner.name], i)
	}

	// Two methods on one path below the object, such as GET and PUT of .../plan-artifact, tell
	// their actions apart by the method's action, except for the GET.
	for _, indexes := range byKind {
		methodsOf := map[string][]string{}
		for _, i := range indexes {
			methods := methodsOf[operations[i].Action]
			if !slices.Contains(methods, operations[i].Method) {
				methodsOf[operations[i].Action] = append(methods, operations[i].Method)
			}
		}
		for _, i := range indexes {
			operation := &operations[i]
			if len(methodsOf[operation.Action]) > 1 && operation.Method != gohttp.MethodGet {
				operation.Action += "-" + methodAction(operation.Method)
			}
		}
	}
}

// action takes the rest of the template after the kind's path.
func action(method string, rest []segment) string {
	var literals []string
	for _, segment := range rest {
		if !segment.isParameter() {
			literals = append(literals, string(segment))
		}
	}
	switch {
	case len(literals) > 0:
		return strings.Join(literals, "-")
	case method == gohttp.MethodGet && len(rest) == 0:
		return "list"
	case method == gohttp.MethodGet:
		return "show"
	default:
		return methodAction(method)
	}
}

func methodAction(method string) string {
	if action, ok := actionOfMethod[method]; ok {
		return action
	}
	return strings.ToLower(method)
}

func startsWith(segments, prefix []segment) bool {
	return len(segments) >= len(prefix) && slices.Equal(segments[:len(prefix)], prefix)
}
