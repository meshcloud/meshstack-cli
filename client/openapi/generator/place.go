package generator

import (
	"cmp"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
	"unicode"
)

// decl is one Go type of a namedType: the only one, or that of one direction.
type decl struct {
	t *namedType
	// dir is zero for a type that serves both directions.
	dir  direction
	home *pkg
	base string
	name string
}

func (g *generator) place() error {
	types := slices.SortedFunc(maps.Values(g.types), func(a, b *namedType) int {
		return cmp.Compare(a.loc.String(), b.loc.String())
	})
	for _, t := range types {
		if t.dropProperty != "" && t.reachedDirectly {
			return fmt.Errorf("%s is a variant of a union and is used on its own, so the union cannot write its %s", t.loc, t.dropProperty)
		}
		t.split = t.ownSplit()
	}
	for changed := true; changed; {
		changed = false
		for _, t := range types {
			if t.split || len(t.reachers) < 2 {
				continue
			}
			for _, dir := range t.dirs() {
				for _, child := range t.children(dir) {
					if child.split && !t.split {
						t.split, changed = true, true
					}
				}
			}
		}
	}

	var decls []*decl
	for _, t := range types {
		t.decls = map[direction]*decl{}
		if t.split {
			for _, dir := range t.dirs() {
				d := &decl{t: t, dir: dir, home: g.home(t.reachers[dir])}
				t.decls[dir] = d
				decls = append(decls, d)
			}
			continue
		}
		reachers := map[*pkg]bool{}
		for _, r := range t.reachers {
			maps.Copy(reachers, r)
		}
		d := &decl{t: t, home: g.home(reachers)}
		for _, dir := range t.dirs() {
			t.decls[dir] = d
		}
		decls = append(decls, d)
	}

	// An inline type takes its name from the type it is in, so components are named first.
	depth := func(d *decl) int {
		if d.t.component != "" {
			return 0
		}
		return 1 + strings.Count(d.t.loc.Property, ".")
	}
	slices.SortStableFunc(decls, func(a, b *decl) int { return cmp.Compare(depth(a), depth(b)) })
	for _, p := range append(slices.Clone(g.packages), g.shared) {
		for _, o := range p.operations {
			p.names[o.queryType] = true
		}
		for _, reserved := range []string{"Client", "NewClient", "Kind", "ApiVersion", "MediaType"} {
			p.names[reserved] = true
		}
	}
	for _, d := range decls {
		if err := g.name(d); err != nil {
			return err
		}
		d.home.types = append(d.home.types, d)
	}
	for _, p := range append(slices.Clone(g.packages), g.shared) {
		slices.SortFunc(p.types, func(a, b *decl) int { return cmp.Compare(a.name, b.name) })
	}
	return nil
}

func (g *generator) home(reachers map[*pkg]bool) *pkg {
	if len(reachers) == 1 {
		for p := range reachers {
			return p
		}
	}
	return g.shared
}

func (g *generator) name(d *decl) error {
	var candidates []string
	switch {
	case d.t.component == "":
		prefix := sanitize(strings.ReplaceAll(d.t.loc.Schema, ".", ""))
		if parent, ok := g.types[d.t.loc.Schema]; ok && parent.decls != nil {
			prefix = cmp.Or(parent.decls[d.dir], parent.decls[response], parent.decls[request]).base
		}
		var suffix strings.Builder
		for property := range strings.SplitSeq(d.t.loc.Property, ".") {
			suffix.WriteString(goName(property))
		}
		candidates = []string{prefix + suffix.String()}
	case d.home == g.shared:
		candidates = []string{sanitize(strings.ReplaceAll(d.t.component, ".", ""))}
	default:
		candidates = d.home.candidates(d.t.component, d.t.reachers)
	}
	for i := range candidates {
		d.base = candidates[i]
		d.name = d.base
		if d.dir != 0 && !strings.HasSuffix(d.name, d.dir.suffix()) {
			d.name += d.dir.suffix()
		}
		if !d.home.names[d.name] {
			d.home.names[d.name] = true
			return nil
		}
	}
	for i := 2; ; i++ {
		if name := d.name + strconv.Itoa(i); !d.home.names[name] {
			d.name = name
			d.home.names[name] = true
			return nil
		}
	}
}

// candidates name a component in a kind's package without the kind and version, shortest first:
// MeshBuildingBlockRun.MeshBuildingBlockRunSpec.Behavior is Behavior, then SpecBehavior.
func (p *pkg) candidates(component string, reachers map[direction]map[*pkg]bool) []string {
	var segments []string
	for segment := range strings.SplitSeq(duplicateSuffixRe.ReplaceAllString(component, ""), ".") {
		segment = strings.TrimPrefix(segment, upperFirst(p.Kind))
		segments = append(segments, sanitize(p.withoutVersion(segment)))
	}
	var candidates []string
	for i := range slices.Backward(segments) {
		if name := strings.Join(segments[i:], ""); name != "" {
			candidates = append(candidates, name)
		}
	}
	if len(candidates) == 0 {
		if _, isResponse := reachers[response]; isResponse {
			return []string{"Response"}
		}
		return []string{"Request"}
	}
	return append(candidates, sanitize(strings.ReplaceAll(component, ".", "")))
}

// withoutVersion drops the version of the package where it stands as a word of its own, as the V2
// of SpecV2Response.
func (p *pkg) withoutVersion(name string) string {
	if p.ApiVersion == "" {
		return name
	}
	number, _, _ := strings.Cut(strings.TrimPrefix(p.ApiVersion, "v"), "-")
	token := "V" + number
	for i := 0; i+len(token) <= len(name); {
		j := strings.Index(name[i:], token)
		if j < 0 {
			break
		}
		end := i + j + len(token)
		if end == len(name) || unicode.IsUpper(rune(name[end])) {
			name = name[:i+j] + name[end:]
			continue
		}
		i = end
	}
	return name
}
