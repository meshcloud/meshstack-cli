package internal

import (
	"encoding/json/jsontext"
	"fmt"
	"io"
	"iter"

	"github.com/goccy/go-yaml"
	"github.com/spf13/pflag"
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

// WriteList writes every item the sequence yields, each one as it arrives, and stops at the first
// error. An empty sequence writes nothing at all.
func WriteList(w io.Writer, format OutputFormat, items iter.Seq2[jsontext.Value, error]) error {
	writeItem, err := itemWriter(format)
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

// itemWriter writes each item as the server sent it rather than as the client's types decode it.
// A round trip through those types drops every member they do not model, apiVersion and kind
// included, writes a nil pointer as a null the server never sent, and loses a variant of a union
// the client does not know yet.
//
// _links stays in as well: it names relations the object's own members do not carry, such as the
// building blocks of a definition, which is how an agent reading a listing finds what to look at
// next.
func itemWriter(format OutputFormat) (func(w io.Writer, item jsontext.Value) error, error) {
	switch format {
	case OutputNdjson:
		return func(w io.Writer, item jsontext.Value) error {
			if err := item.Compact(); err != nil {
				return err
			}
			_, err := fmt.Fprintf(w, "%s\n", item)
			return err
		}, nil
	case OutputYaml:
		separator := ""
		return func(w io.Writer, item jsontext.Value) error {
			document, err := yaml.JSONToYAML(item)
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
