package xurl

import (
	"encoding"
	"errors"
	"fmt"
	"net/url"
)

var (
	_ encoding.TextUnmarshaler = &URL{}
	_ encoding.TextMarshaler   = URL{}
)

type URL struct {
	*url.URL
}

func (u *URL) UnmarshalText(text []byte) (err error) {
	u.URL, err = url.ParseRequestURI(string(text))
	if err == nil && !u.IsAbs() {
		return fmt.Errorf("unmarshaled URL '%s' is not absolute", u)
	}
	return
}

func (u URL) MarshalText() ([]byte, error) {
	if u.URL == nil {
		return nil, errors.New("a zero URL cannot be marshaled; declare an optional URL field as *URL")
	}
	return []byte(u.String()), nil
}

func MustParsef(format string, args ...any) (result URL) {
	if err := result.UnmarshalText([]byte(fmt.Sprintf(format, args...))); err != nil {
		panic(err.Error())
	}
	return
}
