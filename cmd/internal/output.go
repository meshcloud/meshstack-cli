package internal

import (
	"fmt"
	"io"
	"iter"

	"github.com/goccy/go-yaml"
	"github.com/spf13/pflag"

	"github.com/meshcloud/meshstack-cli/internal/json"
)

// OutputFormat is what a list command writes its items as.
type OutputFormat string

const (
	OutputYaml   OutputFormat = "yaml"
	OutputNdjson OutputFormat = "ndjson"
)

// OutputFlag registers --output and holds what it was set to. It implements [pflag.Value], so an
// unknown format is rejected while the flags are parsed rather than after the first page arrived.
type OutputFlag struct{ Format OutputFormat }

func (f *OutputFlag) Register(flags *pflag.FlagSet) {
	f.Format = OutputYaml
	flags.VarP(f, "output", "o", fmt.Sprintf("output format, %s or %s", OutputYaml, OutputNdjson))
}

func (f *OutputFlag) String() string {
	return string(f.Format)
}

func (f *OutputFlag) Set(value string) error {
	format := OutputFormat(value)
	switch format {
	case OutputYaml, OutputNdjson:
		f.Format = format
		return nil
	default:
		return fmt.Errorf("%q is no output format, write %s or %s", value, OutputYaml, OutputNdjson)
	}
}

func (f *OutputFlag) Type() string {
	return "format"
}

// WriteItem writes a single item, for a command whose result is one object rather than a listing.
func WriteItem[T any](w io.Writer, format OutputFormat, item T) error {
	writeItem, err := itemWriter[T](format)
	if err != nil {
		return err
	}
	return writeItem(w, item)
}

// WriteList writes every item the sequence yields, each one as it arrives, and stops at the first
// error. An empty sequence writes nothing at all.
func WriteList[T any](w io.Writer, format OutputFormat, items iter.Seq2[T, error]) error {
	writeItem, err := itemWriter[T](format)
	if err != nil {
		return err
	}
	for item, itemErr := range items {
		if itemErr != nil {
			return itemErr
		}
		if err := writeItem(w, item); err != nil {
			return err
		}
	}
	return nil
}

// itemWriter marshals through internal/json for both formats, so that the two say the same thing
// about the same object. go-yaml's own struct walk reads the `json` tags but not the rest of the
// contract the client's types are written against: it names an embedded struct as a member of its
// own rather than inlining it, and it passes a MarshalJSON by.
func itemWriter[T any](format OutputFormat) (func(w io.Writer, item T) error, error) {
	switch format {
	case OutputNdjson:
		return func(w io.Writer, item T) error {
			encoded, err := json.Marshal(item)
			if err != nil {
				return err
			}
			_, err = fmt.Fprintf(w, "%s\n", encoded)
			return err
		}, nil
	case OutputYaml:
		separator := ""
		return func(w io.Writer, item T) error {
			encoded, err := json.Marshal(item)
			if err != nil {
				return err
			}
			document, err := jsonToYaml(encoded)
			if err != nil {
				return err
			}
			if _, err := fmt.Fprintf(w, "%s%s", separator, document); err != nil {
				return err
			}
			separator = "---\n"
			return nil
		}, nil
	default:
		return nil, fmt.Errorf("%q is no output format, write %s or %s", format, OutputYaml, OutputNdjson)
	}
}

// jsonToYaml is [yaml.JSONToYAML] with the literal style switched on, which the exported function
// takes no options for. Without it a multi-line string whose first line opens with an indicator
// character — a building block run step's log starts with terraform's `{` banner — is written as
// one escaped line rather than as a block scalar. UseOrderedMap keeps the members in the order the
// json carries them, as yaml.JSONToYAML does.
func jsonToYaml(encoded []byte) ([]byte, error) {
	var document any
	if err := yaml.UnmarshalWithOptions(encoded, &document, yaml.UseOrderedMap()); err != nil {
		return nil, err
	}
	return yaml.MarshalWithOptions(document, yaml.UseLiteralStyleIfMultiline(true))
}
