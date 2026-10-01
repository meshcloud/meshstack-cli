package markdown

import (
	"bytes"
	_ "embed"
	"fmt"
	"io"
	"os"
	"strings"
	"text/template"
	"time"

	"charm.land/glamour/v2"
	"charm.land/glamour/v2/styles"
	"github.com/charmbracelet/x/term"
)

//go:embed credential.md.tmpl
var credentialTemplate string

// Parse makes {{template "credential" .}} available to text, which renders an internal/auth.CredentialStatus.
func Parse(name, text string) *template.Template {
	funcs := template.FuncMap{
		"cell": func(value any) string {
			escaped := strings.ReplaceAll(fmt.Sprint(value), "|", `\|`)
			return strings.Join(strings.Fields(escaped), " ")
		},
		"at": func(t time.Time) string {
			return fmt.Sprintf("%s (%s)", t.Format("2006-01-02 15:04"), relative(time.Until(t)))
		},
	}
	return template.Must(template.Must(template.New(name).Funcs(funcs).Parse(credentialTemplate)).Parse(text))
}

func relative(d time.Duration) string {
	past := d < 0
	d = d.Abs()
	var amount string
	switch {
	case d < time.Minute:
		return "now"
	case d < time.Hour:
		amount = fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 48*time.Hour:
		amount = fmt.Sprintf("%dh", int(d.Hours()))
	default:
		amount = fmt.Sprintf("%dd", int(d.Hours()/24))
	}
	if past {
		return amount + " ago"
	}
	return "in " + amount
}

func Write(w io.Writer, t *template.Template, data any) error {
	text, err := Execute(t, data)
	if err != nil {
		return err
	}
	if file, ok := w.(*os.File); ok && term.IsTerminal(file.Fd()) {
		width, _, sizeErr := term.GetSize(file.Fd())
		if sizeErr != nil {
			width = maxWidth
		}
		if text, err = Render(text, width); err != nil {
			return err
		}
	}
	_, err = io.WriteString(w, text)
	return err
}

// maxWidth keeps the tables readable on a wide terminal.
const maxWidth = 100

func Execute(t *template.Template, data any) (string, error) {
	var out bytes.Buffer
	err := t.Execute(&out, data)
	return out.String(), err
}

// RenderInline takes only a built-in glamour style from GLAMOUR_STYLE, unlike Render: it has to
// change the style's margin, so a GLAMOUR_STYLE that names a style file gets the dark style.
func RenderInline(text string) string {
	style, found := styles.DefaultStyles[os.Getenv("GLAMOUR_STYLE")]
	if !found {
		style = styles.DefaultStyles[styles.DarkStyle]
	}
	inline := *style
	inline.Document.Margin = new(uint(0))
	inline.Document.BlockPrefix, inline.Document.BlockSuffix = "", ""
	// Without word wrap, glamour does not pad the line to the wrap width.
	renderer, err := glamour.NewTermRenderer(glamour.WithStyles(inline), glamour.WithWordWrap(0))
	if err != nil {
		return text
	}
	rendered, err := renderer.Render(text)
	if err != nil {
		return text
	}
	return strings.TrimSuffix(rendered, "\n")
}

// Render uses glamour's dark style unless GLAMOUR_STYLE names another.
func Render(text string, width int) (string, error) {
	renderer, err := glamour.NewTermRenderer(glamour.WithEnvironmentConfig(), glamour.WithWordWrap(min(width, maxWidth)), glamour.WithInlineTableLinks(true))
	if err != nil {
		return "", err
	}
	return renderer.Render(text)
}
