package browser

import (
	"context"
	_ "embed"
	"html/template"
	"log/slog"
	gohttp "net/http"
)

//go:embed callback.html
var callbackPage string

var resultPage = template.Must(template.New("callback").Parse(callbackPage))

func page(ctx context.Context, w gohttp.ResponseWriter, status int, title, message string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if err := resultPage.Execute(w, struct{ Title, Message string }{title, message}); err != nil {
		slog.DebugContext(ctx, "cannot write the login result page", "error", err)
	}
}
