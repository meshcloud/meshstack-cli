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
	"path/filepath"
)

type MarshalerTo = json.MarshalerTo

func Marshal(in any, opts ...Options) ([]byte, error) {
	return json.Marshal(in, opts...)
}

func MarshalWrite(out io.Writer, in any, opts ...Options) error {
	return json.MarshalWrite(out, in, opts...)
}

func MarshalEncode(out *jsontext.Encoder, in any, opts ...Options) error {
	return json.MarshalEncode(out, in, opts...)
}

type MarshalOption func(opts *marshalOptions)

type marshalOptions struct {
	perm os.FileMode
}

// UserOnlyFilePerms is for a file that holds a credential, or a token minted from one.
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
	return json.MarshalWrite(out, payload, json.Deterministic(true), jsontext.WithIndent("  "))
}
