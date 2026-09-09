package browser

import (
	_ "embed"
	"html/template"
	"log/slog"
	"net/http"
)

// callbackPage is deliberately plain: it is shown once, in a tab the user closes immediately.
// It is a file of its own so that an editor treats it as the HTML it is.
//
//go:embed callback.html
var callbackPage string

var resultPage = template.Must(template.New("callback").Parse(callbackPage))

// page is the last thing a login shows: whatever the redirect turned out to be, written into
// the tab the person is still looking at.
func page(w http.ResponseWriter, status int, title, message string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if err := resultPage.Execute(w, struct{ Title, Message string }{title, message}); err != nil {
		slog.Debug("cannot write the login result page", "error", err)
	}
}
