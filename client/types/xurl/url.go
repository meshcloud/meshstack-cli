package xurl

import (
	"encoding"
	"errors"
	"fmt"
	"net/url"
	"strings"
)

var (
	_ encoding.TextUnmarshaler = &URL{}
	_ encoding.TextMarshaler   = URL{}
)

type URL struct {
	*url.URL
}

// UnmarshalText validates and canonicalizes the URL as well.
//
//goland:noinspection GoMixedReceiverTypes
func (u *URL) UnmarshalText(text []byte) (err error) {
	u.URL, err = url.ParseRequestURI(string(text))
	if err != nil {
		return
	}
	if u.Path == "/" {
		// ignore single trailing slash
		u.Path = ""
	}
	if !u.IsAbs() {
		return fmt.Errorf("unmarshaled URL '%s' is not absolute", u)
	}
	u.Host = strings.ToLower(u.Host)
	return
}

// Equal compares both URLs whole.
func (u URL) Equal(other URL) bool {
	if u.URL == nil || other.URL == nil {
		return false
	}
	return u.String() == other.String()
}

func (u URL) MarshalText() ([]byte, error) {
	if u.URL == nil {
		return nil, errors.New("a zero URL cannot be marshaled; declare an optional URL field as *URL")
	}
	return []byte(u.String()), nil
}

func MustParsef(format string, args ...any) (result URL) {
	if err := result.UnmarshalText([]byte(fmt.Sprintf(format, args...))); err != nil {
		panic(err)
	}
	return
}
