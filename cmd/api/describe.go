package api

import (
	_ "embed"
	"fmt"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/meshcloud/meshstack-cli/client/openapi"
	"github.com/meshcloud/meshstack-cli/cmd/internal"
	"github.com/meshcloud/meshstack-cli/cmd/internal/markdown"
)

//go:embed describe.md.tmpl
var describeTemplateText string

var describeTemplate = markdown.Parse("describe", describeTemplateText)

const describeFlagName = "describe"

type described struct {
	Title      string
	Operations []describedOperation
}

type describedOperation struct {
	openapi.Operation
	openapi.OperationDoc
}

// describeSelector reads a kind, as its command or an alias of it names the kind, and an action
// after a dot: buildingblock, bb.list.
func describeSelector(spec openapi.Spec, value string) (openapi.Selector, error) {
	name, action, _ := strings.Cut(value, ".")
	var commands []string
	for _, kind := range spec.Kinds() {
		command := internal.KindCommand(kind)
		commands = append(commands, command)
		if command != internal.KindCommand(name) {
			continue
		}
		if actions := spec.Actions(kind); action != "" && !slices.Contains(actions, action) {
			return openapi.Selector{}, fmt.Errorf("the API docs list no action %q of %s, write one of %s",
				action, kind, strings.Join(actions, ", "))
		}
		return openapi.Selector{Kind: kind, Action: action}, nil
	}
	return openapi.Selector{}, fmt.Errorf("the API docs list no meshObject kind %q, write one of %s",
		name, strings.Join(commands, ", "))
}

func writeKindDescription(cmd *cobra.Command, spec openapi.Spec, value string, output internal.ShowFlag) error {
	selector, err := describeSelector(spec, value)
	if err != nil {
		return err
	}
	selected, err := spec.Select(selector)
	if err != nil {
		return err
	}
	return writeDescription(cmd, selected, selector.Kind, output)
}

// writeRequestDescription titles the operations of a path by their kind, as --describe does, and
// by the path where they are of no kind.
func writeRequestDescription(cmd *cobra.Command, selected openapi.Spec, path string, output internal.ShowFlag) error {
	title := path
	if kinds := selected.Kinds(); len(kinds) == 1 {
		title = kinds[0]
	}
	return writeDescription(cmd, selected, title, output)
}

func writeDescription(cmd *cobra.Command, selected openapi.Spec, title string, output internal.ShowFlag) error {
	if output.Json() {
		return writeJson(cmd, selected)
	}
	d := described{Title: title}
	for _, operation := range selected.Operations {
		doc, err := selected.Doc(operation)
		if err != nil {
			return fmt.Errorf("cannot read %s %s: %w", operation.Method, operation.PathTemplate, err)
		}
		d.Operations = append(d.Operations, describedOperation{operation, doc})
	}
	return markdown.Write(cmd.OutOrStdout(), describeTemplate, d)
}
