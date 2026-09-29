package browser

import (
	"context"
	_ "embed"
	"encoding/base64"
	"html/template"
	"log/slog"
	gohttp "net/http"

	"github.com/meshcloud/meshstack-cli/internal/meshstack"
)

var (
	//go:embed page.html
	pageHtml string
	//go:embed logo.svg
	logoSvg []byte
)

var (
	pageTemplate = template.Must(template.New("page").Parse(pageHtml))
	// A data URI, so the page needs nothing else from the loopback server.
	logo = template.URL("data:image/svg+xml;base64," + base64.StdEncoding.EncodeToString(logoSvg)) //nolint:gosec // the embedded logo, not input
)

type pageData struct {
	Logo           template.URL
	Title, Message string
	Failed         bool
	Form           *accessLevelForm
}

type accessLevelForm struct {
	Nonce  string
	Levels []accessLevelOption
}

type accessLevelOption struct {
	Value, Label, Detail string
	Selected             bool
}

func newAccessLevelForm(nonce string, selected meshstack.AccessLevel) *accessLevelForm {
	form := &accessLevelForm{Nonce: nonce}
	for _, level := range meshstack.AccessLevels {
		form.Levels = append(form.Levels, accessLevelOption{
			Value:    string(level),
			Label:    level.Label(),
			Detail:   level.Detail(),
			Selected: level == selected,
		})
	}
	return form
}

func page(ctx context.Context, w gohttp.ResponseWriter, status int, data pageData) {
	data.Logo = logo
	data.Failed = status >= gohttp.StatusBadRequest
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if err := pageTemplate.Execute(w, data); err != nil {
		slog.DebugContext(ctx, "cannot write the login page", "error", err)
	}
}
