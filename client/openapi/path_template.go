package openapi

import (
	"slices"
	"strings"
)

type PathTemplate string

type segment string

func (t PathTemplate) segments() []segment {
	return splitPath(string(t))
}

func (t PathTemplate) matches(requestPath string) bool {
	return slices.EqualFunc(t.segments(), splitPath(requestPath), func(templateSegment, pathSegment segment) bool {
		return pathSegment != "" && (templateSegment.isParameter() || templateSegment == pathSegment)
	})
}

func (t PathTemplate) literalStart() []segment {
	segments := t.segments()
	if i := slices.IndexFunc(segments, segment.isParameter); i >= 0 {
		return segments[:i]
	}
	return segments
}

func splitPath(path string) []segment {
	var segments []segment
	for s := range strings.SplitSeq(strings.Trim(path, "/"), "/") {
		segments = append(segments, segment(s))
	}
	return segments
}

func (s segment) isParameter() bool {
	return strings.HasPrefix(string(s), "{") && strings.HasSuffix(string(s), "}")
}
