package internal

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"fmt"
	"io"
	"iter"
	"text/template"

	"github.com/spf13/pflag"

	"github.com/meshcloud/meshstack-cli/cmd/internal/markdown"
)

type OutputFormat string

const (
	OutputJson     OutputFormat = "json"
	OutputNdjson   OutputFormat = "ndjson"
	OutputMarkdown OutputFormat = "markdown"
)

// OutputFlag implements [pflag.Value], so an unknown format is rejected while the flags are parsed
// rather than after the first page arrived.
type OutputFlag struct{ Format OutputFormat }

func (f *OutputFlag) Register(flags *pflag.FlagSet) {
	f.Format = OutputJson
	flags.VarP(f, "output", "o", fmt.Sprintf("output format: %s for one array, %s for one item per line", OutputJson, OutputNdjson))
}

// RegisterForItem is Register for a command that writes one object through [WriteItem].
func (f *OutputFlag) RegisterForItem(flags *pflag.FlagSet) {
	f.Format = OutputJson
	flags.VarP(f, "output", "o", fmt.Sprintf("output format: %s, or %s for one line", OutputJson, OutputNdjson))
}

func (f *OutputFlag) String() string {
	return string(f.Format)
}

func (f *OutputFlag) Set(value string) error {
	format := OutputFormat(value)
	switch format {
	case OutputJson, OutputNdjson:
		f.Format = format
		return nil
	default:
		return fmt.Errorf("%q is no output format, write %s or %s", value, OutputJson, OutputNdjson)
	}
}

func (f *OutputFlag) Type() string {
	return "format"
}

// WriteList leaves a listing that fails part way through unterminated, so that json output which
// stopped early does not parse as a complete array.
func WriteList(w io.Writer, format OutputFormat, items iter.Seq2[jsontext.Value, error]) error {
	list, err := listFormatOf(format)
	if err != nil {
		return err
	}
	written := 0
	for item, itemErr := range items {
		if itemErr != nil {
			return itemErr
		}
		separator := list.between
		if written == 0 {
			separator = list.begin
		}
		if err = list.format(&item); err != nil {
			return err
		}
		if _, err = fmt.Fprintf(w, "%s%s%s", separator, item, list.after); err != nil {
			return err
		}
		written++
	}
	end := list.end
	if written == 0 {
		end = list.empty
	}
	_, err = io.WriteString(w, end)
	return err
}

func unknownFormat(format OutputFormat) error {
	return fmt.Errorf("%q is no output format, write %s or %s", format, OutputJson, OutputNdjson)
}

type listFormat struct {
	begin, between, after, end, empty string
	format                            func(item *jsontext.Value) error
}

func listFormatOf(format OutputFormat) (listFormat, error) {
	switch format {
	case OutputNdjson:
		return listFormat{
			after:  "\n",
			format: func(item *jsontext.Value) error { return item.Compact() },
		}, nil
	case OutputJson:
		return listFormat{
			begin: "[\n  ", between: ",\n  ", end: "\n]\n", empty: "[]\n",
			format: func(item *jsontext.Value) error {
				return item.Indent(jsontext.WithIndentPrefix("  "), jsontext.WithIndent("  "))
			},
		}, nil
	default:
		return listFormat{}, unknownFormat(format)
	}
}

type ShowFlag struct{ Format OutputFormat }

func (f *ShowFlag) Register(flags *pflag.FlagSet) {
	f.Format = OutputMarkdown
	flags.VarP(f, "output", "o", fmt.Sprintf("output format: %s, or %s for scripts", OutputMarkdown, OutputJson))
}

func (f *ShowFlag) String() string {
	return string(f.Format)
}

func (f *ShowFlag) Set(value string) error {
	format := OutputFormat(value)
	switch format {
	case OutputMarkdown, OutputJson:
		f.Format = format
		return nil
	default:
		return fmt.Errorf("%q is no output format, write %s or %s", value, OutputMarkdown, OutputJson)
	}
}

func (f *ShowFlag) Type() string {
	return "format"
}

// Json output is read by a script, so a command must not prompt for anything then.
func (f *ShowFlag) Json() bool {
	return f.Format == OutputJson
}

func (f *ShowFlag) Show(w io.Writer, markdownTemplate *template.Template, data any) error {
	if f.Json() {
		content, err := json.Marshal(data, jsontext.WithIndent("  "))
		if err != nil {
			return err
		}
		_, err = fmt.Fprintf(w, "%s\n", content)
		return err
	}
	return markdown.Write(w, markdownTemplate, data)
}

// WriteItem is for a command that answers with one object rather than a listing.
func WriteItem(w io.Writer, format OutputFormat, item jsontext.Value) error {
	var err error
	switch format {
	case OutputNdjson:
		err = item.Compact()
	case OutputJson:
		err = item.Indent(jsontext.WithIndent("  "))
	default:
		err = unknownFormat(format)
	}
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "%s\n", item)
	return err
}
