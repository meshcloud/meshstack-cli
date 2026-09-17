package version

import (
	"cmp"
	"encoding"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

var (
	_ encoding.TextUnmarshaler = &Version{}
	_ encoding.TextMarshaler   = Version{}
)

type Version struct {
	Major, Minor, Patch int
	// Extra is everything past the first '-', which a CLI version carries and a meshStack
	// version does not: a prerelease, or the commit in a version the go command stamped.
	Extra string
}

const separatorExtra = "-"

func Parse(s string) (Version, error) {
	parts := strings.Split(strings.TrimPrefix(s, "v"), ".")
	if len(parts) != 3 {
		return Version{}, fmt.Errorf("cannot parse '%s' as version: expected 3, got %d fields separated by '.'", s, len(parts))
	}
	patch, extra, _ := strings.Cut(parts[2], separatorExtra)
	parts[2] = patch
	var errs []error
	partTo := func(i int, target *int) {
		parsed, err := strconv.Atoi(parts[i])
		if err == nil && parsed < 0 {
			err = fmt.Errorf("negative number '%d' not allowed", parsed)
		}
		if err != nil {
			errs = append(errs, fmt.Errorf("part i=%d: %w", i, err))
		} else {
			*target = parsed
		}
	}
	var result Version
	partTo(0, &result.Major)
	partTo(1, &result.Minor)
	partTo(2, &result.Patch)
	if len(errs) > 0 {
		return Version{}, fmt.Errorf("cannot parse '%s' as version: %w", s, errors.Join(errs...))
	}
	result.Extra = extra
	return result, nil
}

func MustParse(s string) Version {
	version, err := Parse(s)
	if err != nil {
		panic(err)
	}
	return version
}

func (v Version) Compare(other Version) int {
	if major := cmp.Compare(v.Major, other.Major); major != 0 {
		return major
	} else if minor := cmp.Compare(v.Minor, other.Minor); minor != 0 {
		return minor
	} else if patch := cmp.Compare(v.Patch, other.Patch); patch != 0 {
		return patch
	}
	// Deliberately not semver, which ranks a prerelease below its release: here the version
	// carrying an Extra is the greater one, so a build from a tagged commit outranks the tag.
	return strings.Compare(v.Extra, other.Extra)
}

func (v Version) Less(other Version) bool {
	return v.Compare(other) < 0
}

func (v Version) String() (s string) {
	s = fmt.Sprintf("%d.%d.%d", v.Major, v.Minor, v.Patch)
	if v.Extra != "" {
		s += separatorExtra + v.Extra
	}
	return
}

func (v Version) MarshalText() ([]byte, error) {
	return []byte(v.String()), nil
}

//goland:noinspection GoMixedReceiverTypes
func (v *Version) UnmarshalText(text []byte) (err error) {
	*v, err = Parse(string(text))
	return
}
