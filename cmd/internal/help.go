package internal

import (
	"io"
	"os"
	"regexp"
	"strings"
	"text/template"
	"unicode"

	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/term"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

const (
	// helpWidthWithoutTerminal keeps help in a pipe or a test the same on every machine.
	helpWidthWithoutTerminal = 80
	// maxHelpWidth keeps prose readable on a wide terminal.
	maxHelpWidth = 100
)

// The templates are cobra's defaults, with the long description and the flags wrapped to a width
// that comes from where the help is written. Cobra's template functions cannot know that writer.
var helpTemplates = template.Must(template.New("help").Funcs(template.FuncMap{
	"wrap": wrapHelp,
	"flagUsages": func(flags *pflag.FlagSet, width int) string {
		return strings.TrimRightFunc(flags.FlagUsagesWrapped(width), unicode.IsSpace)
	},
	"rpad": func(s string, padding int) string {
		return s + strings.Repeat(" ", max(padding-len(s), 0))
	},
}).Parse(`{{with or .Long .Short}}{{wrap . $.Width}}

{{end}}{{if or .Runnable .HasSubCommands}}{{template "usage" .}}{{end}}
{{- define "usage"}}Usage:{{if .Runnable}}
  {{.UseLine}}{{end}}{{if .HasAvailableSubCommands}}
  {{.CommandPath}} [command]{{end}}{{if gt (len .Aliases) 0}}

Aliases:
  {{.NameAndAliases}}{{end}}{{if .HasExample}}

Examples:
{{.Example}}{{end}}{{if .HasAvailableSubCommands}}{{$cmds := .Commands}}{{if eq (len .Groups) 0}}

Available Commands:{{range $cmds}}{{if (or .IsAvailableCommand (eq .Name "help"))}}
  {{rpad .Name .NamePadding }} {{.Short}}{{end}}{{end}}{{else}}{{range $group := .Groups}}

{{.Title}}{{range $cmds}}{{if (and (eq .GroupID $group.ID) (or .IsAvailableCommand (eq .Name "help")))}}
  {{rpad .Name .NamePadding }} {{.Short}}{{end}}{{end}}{{end}}{{if not .AllChildCommandsHaveGroup}}

Additional Commands:{{range $cmds}}{{if (and (eq .GroupID "") (or .IsAvailableCommand (eq .Name "help")))}}
  {{rpad .Name .NamePadding }} {{.Short}}{{end}}{{end}}{{end}}{{end}}{{end}}{{if .HasAvailableLocalFlags}}

Flags:
{{flagUsages .LocalFlags .Width}}{{end}}{{if .HasAvailableInheritedFlags}}

Global Flags:
{{flagUsages .InheritedFlags .Width}}{{end}}{{if .HasHelpSubCommands}}

Additional help topics:{{range .Commands}}{{if .IsAdditionalHelpTopicCommand}}
  {{rpad .CommandPath .CommandPathPadding}} {{.Short}}{{end}}{{end}}{{end}}{{if .HasAvailableSubCommands}}

Use "{{.CommandPath}} [command] --help" for more information about a command.{{end}}
{{end}}`))

type helpData struct {
	*cobra.Command

	Width int
}

// WrapHelpToTerminal makes the help and the usage of root and its subcommands fit the terminal
// they are written to.
func WrapHelpToTerminal(root *cobra.Command) {
	root.SetHelpFunc(func(cmd *cobra.Command, _ []string) {
		if err := writeHelp(cmd.OutOrStdout(), "help", cmd); err != nil {
			cmd.PrintErrln(err)
		}
	})
	root.SetUsageFunc(func(cmd *cobra.Command) error {
		return writeHelp(cmd.OutOrStderr(), "usage", cmd)
	})
}

func writeHelp(w io.Writer, name string, cmd *cobra.Command) error {
	width := helpWidthWithoutTerminal
	if file, ok := w.(*os.File); ok {
		if terminalWidth, _, err := term.GetSize(file.Fd()); err == nil {
			width = min(terminalWidth, maxHelpWidth)
		}
	}
	return helpTemplates.ExecuteTemplate(w, name, helpData{cmd, width})
}

var listItem = regexp.MustCompile(`^\s*(?:[-*]|\d+\.)\s+`)

// wrapHelp wraps each line of text on its own, so a line break in the text always stays one. A list
// item continues under its own text, and any other indented line, such as code, is left as it is.
// It breaks only at spaces, because ansi.Wordwrap also breaks a flag such as --mode at its hyphens.
func wrapHelp(text string, width int) string {
	lines := strings.Split(strings.TrimRightFunc(text, unicode.IsSpace), "\n")
	for i, line := range lines {
		marker := listItem.FindString(line)
		if marker == "" && strings.TrimLeftFunc(line, unicode.IsSpace) != line {
			continue
		}
		var wrapped strings.Builder
		wrapped.WriteString(marker)
		lineWidth := len(marker)
		for j, word := range strings.Fields(line[len(marker):]) {
			wordWidth := ansi.StringWidth(word)
			switch {
			case j == 0:
			case lineWidth+1+wordWidth > width:
				wrapped.WriteString("\n" + strings.Repeat(" ", len(marker)))
				lineWidth = len(marker)
			default:
				wrapped.WriteByte(' ')
				lineWidth++
			}
			wrapped.WriteString(word)
			lineWidth += wordWidth
		}
		lines[i] = wrapped.String()
	}
	return strings.Join(lines, "\n")
}
