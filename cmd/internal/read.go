package internal

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// JsonInputFlag names a JSON file to read into a T, or - for stdin.
type JsonInputFlag[T any] struct {
	name   string
	Source string
}

func (f *JsonInputFlag[T]) Register(flags *pflag.FlagSet, name, usage string) {
	f.name = name
	flags.Var(f, name, usage)
}

func (f *JsonInputFlag[T]) String() string {
	return f.Source
}

func (f *JsonInputFlag[T]) Set(source string) error {
	f.Source = source
	return nil
}

func (f *JsonInputFlag[T]) Type() string {
	return "string"
}

func (f *JsonInputFlag[T]) Read(cmd *cobra.Command) (value T, err error) {
	var read jsontext.Value
	if f.Source == "-" {
		read, err = io.ReadAll(cmd.InOrStdin())
	} else {
		read, err = os.ReadFile(f.Source)
	}
	switch {
	case err != nil:
		return value, err
	case !read.IsValid():
		return value, fmt.Errorf("--%s %s is no JSON", f.name, f.Source)
	}
	if err = json.Unmarshal(read, &value); err != nil {
		return value, fmt.Errorf("--%s %s holds JSON of another shape: %w", f.name, f.Source, err)
	}
	return value, nil
}
