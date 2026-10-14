package json

import (
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"log/slog"
	"os"
)

func Unmarshal(in []byte, out any) error {
	return json.Unmarshal(in, out)
}

func UnmarshalFrom(ctx context.Context, file string, out any, unmarshalers ...*json.Unmarshalers) (err error) {
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
	err = json.UnmarshalDecode(jsontext.NewDecoder(f), &out,
		json.WithUnmarshalers(json.JoinUnmarshalers(unmarshalers...)),
	)
	return
}

func ModifyAfterUnmarshal[T any](modifier func(target *T)) *json.Unmarshalers {
	var decoding bool
	return json.UnmarshalFromFunc(func(decoder *jsontext.Decoder, t *T) error {
		if decoding {
			// The nested decode below dispatches here again, and json.JoinUnmarshalers reads
			// errors.ErrUnsupported as "leave this value to the default behavior".
			return errors.ErrUnsupported
		}
		decoding = true
		defer func() { decoding = false }()
		if err := json.UnmarshalDecode(decoder, t); err != nil {
			return err
		}
		modifier(t)
		return nil
	})
}
