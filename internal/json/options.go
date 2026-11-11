package json

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
)

type (
	Options      = json.Options
	Unmarshalers = json.Unmarshalers
)

func JoinOptions(opts ...Options) Options {
	return json.JoinOptions(opts...)
}

func Deterministic(v bool) Options {
	return json.Deterministic(v)
}

func FormatNilSliceAsNull(v bool) Options {
	return json.FormatNilSliceAsNull(v)
}

func FormatNilMapAsNull(v bool) Options {
	return json.FormatNilMapAsNull(v)
}

func WithUnmarshalers(unmarshalers ...*Unmarshalers) Options {
	joined := json.JoinUnmarshalers(unmarshalers...)
	if joined == nil {
		// json/v2 dereferences a nil *Unmarshalers when it decodes into an any.
		return nil
	}
	return json.WithUnmarshalers(joined)
}

func UnmarshalFromFunc[T any](fn func(*jsontext.Decoder, T) error) *Unmarshalers {
	return json.UnmarshalFromFunc(fn)
}
