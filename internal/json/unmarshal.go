package json

import (
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
)

type UnmarshalerFrom = json.UnmarshalerFrom

func Unmarshal(in []byte, out any, opts ...Options) error {
	return json.Unmarshal(in, out, opts...)
}

func UnmarshalRead(in io.Reader, out any, opts ...Options) error {
	return json.UnmarshalRead(in, out, opts...)
}

func UnmarshalDecode(in *jsontext.Decoder, out any, opts ...Options) error {
	return json.UnmarshalDecode(in, out, opts...)
}

func UnmarshalFrom(ctx context.Context, file string, out any, opts ...Options) (err error) {
	var f *os.File
	//nolint:gosec // G304: internal/config builds every path here from the config dir and validated names
	f, err = os.Open(file)
	if err != nil {
		return err
	}
	defer func() {
		err = errors.Join(err, f.Close())
		if err != nil {
			err = fmt.Errorf("cannot unmarshal json from %s: %w", file, err)
		} else {
			slog.DebugContext(ctx, "Unmarshaled json from "+file)
		}
	}()
	return UnmarshalRead(f, out, opts...)
}

func ModifyAfterUnmarshal[T any](modifier func(target *T)) Options {
	var decoding bool
	return WithUnmarshalers(UnmarshalFromFunc(func(decoder *jsontext.Decoder, t *T) error {
		if decoding {
			// The nested decode below dispatches here again, and json.JoinUnmarshalers reads
			// errors.ErrUnsupported as "leave this value to the default behavior".
			return errors.ErrUnsupported
		}
		decoding = true
		defer func() { decoding = false }()
		if err := UnmarshalDecode(decoder, t); err != nil {
			return err
		}
		modifier(t)
		return nil
	}))
}
