package markdown

import (
	"bytes"
	"cmp"
	_ "embed"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"text/template"
	"time"
	"unicode"

	"charm.land/glamour/v2"
	"charm.land/glamour/v2/styles"
	"github.com/charmbracelet/x/ansi"
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
		// indent writes no-break spaces, because goldmark trims the spaces a table cell starts with.
		"indent": func(depth int) string {
			return strings.Repeat("\u00a0\u00a0", depth)
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

// Write wraps at the narrowest width that takes no more lines than the terminal's does, because
// glamour stretches a table to the width it wraps at, and a short table reads badly across a wide
// terminal.
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
		if text, err = renderFitting(text, width); err != nil {
			return err
		}
	}
	_, err = io.WriteString(w, text)
	return err
}

// renderFitting compares the words as well as the lines, because glamour cuts off what does not fit
// into a table cell rather than wrap it.
func renderFitting(text string, width int) (string, error) {
	type shape struct {
		lines int
		words string
	}
	var err error
	shapeAt := func(width int) shape {
		rendered, renderErr := render(text, width)
		err = cmp.Or(err, renderErr)
		return shape{strings.Count(rendered, "\n"), strings.Join(strings.FieldsFunc(ansi.Strip(rendered), isSpaceOrBorder), " ")}
	}
	full := shapeAt(width)
	// glamour does not wrap at 0 at all, so the search starts at 1.
	narrowest := 1 + sort.Search(width-1, func(i int) bool { return shapeAt(1+i) == full })
	if err != nil {
		return "", err
	}
	return render(text, narrowest)
}

// isSpaceOrBorder leaves the borders of a table out of its words, since a border is as long as the
// width. glamour draws them from the Unicode block Box Drawing.
func isSpaceOrBorder(r rune) bool {
	return unicode.IsSpace(r) || '\u2500' <= r && r <= '\u257f'
}

// maxWidth keeps a table of a full-screen prompt readable on a wide terminal.
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

// Render is for a full-screen prompt. It uses glamour's dark style unless GLAMOUR_STYLE names
// another, and wraps at maxWidth at most.
func Render(text string, width int) (string, error) {
	return render(text, min(width, maxWidth))
}

func render(text string, width int) (string, error) {
	renderer, err := glamour.NewTermRenderer(glamour.WithEnvironmentConfig(), glamour.WithWordWrap(width), glamour.WithInlineTableLinks(true))
	if err != nil {
		return "", err
	}
	return renderer.Render(text)
}
