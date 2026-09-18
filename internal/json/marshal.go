package json

import (
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
)

// wireCompatibility holds the output to the shape encoding/json v1 produced: a nil slice and a
// nil map go out as null rather than as [] and {}, and object members are sorted. json/v2 would
// write [] and {} and leave map order to chance, and neither change is ours to make — the
// meshObject API may read null and an empty collection differently on a PUT, and the Terraform
// provider hashes this JSON to decide whether a resource changed.
var wireCompatibility = json.JoinOptions(
	json.Deterministic(true),
	json.FormatNilSliceAsNull(true),
	json.FormatNilMapAsNull(true),
)

func Marshal(payload any) ([]byte, error) {
	return json.Marshal(payload, wireCompatibility)
}

type MarshalOption func(opts *marshalOptions)

type marshalOptions struct {
	perm os.FileMode
}

// UserOnlyFilePerms keeps the file readable and writable by its owner alone. Use it for a
// file that holds a credential, or a token minted from one.
func UserOnlyFilePerms() MarshalOption {
	return func(opts *marshalOptions) {
		opts.perm = 0o600
	}
}

func MarshalTo(ctx context.Context, file string, payload any, options ...MarshalOption) (err error) {
	opts := marshalOptions{perm: 0o644}
	for _, option := range options {
		option(&opts)
	}
	if err = os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
		return err
	}
	// Every reader of these files reads them without taking the lock, so none of them may
	// ever see a half-written one: the encoder fills a temporary file in the same directory,
	// and the rename below publishes it in one step.
	var out *os.File
	out, err = os.CreateTemp(filepath.Dir(file), filepath.Base(file)+".tmp")
	if err != nil {
		return err
	}
	defer func() {
		err = errors.Join(err, out.Chmod(opts.perm), out.Close())
		if err == nil {
			err = os.Rename(out.Name(), file)
		}
		if err != nil {
			_ = os.Remove(out.Name())
			err = fmt.Errorf("cannot marshal json to %s: %w", file, err)
		} else {
			slog.DebugContext(ctx, "Marshaled json to "+file)
		}
	}()
	encoder := jsontext.NewEncoder(out, jsontext.WithIndent("  "))
	return json.MarshalEncode(encoder, payload, wireCompatibility)
}
