// Package diags carries an error that a rich front end can render.
//
// pkg/auth returns these so that no caller has to parse a string: the meshStack CLI
// prints a summary and a paragraph of advice, and the Terraform provider turns the
// same two fields into a diagnostic. A Problem carries no severity, because every
// Problem stops the work — pkg/auth's package doc says why a non-error never comes
// back this way.
package diags

import (
	"fmt"
)

// Problem is an error that already carries the parts a rich front end wants: a title
// and an actionable paragraph.
//
// It carries no severity. A Problem is always fatal, because an error return is the
// only thing pkg/ hands back to a front end — see the package doc on pkg/auth.
//
// It deliberately does not implement terraform-plugin-framework's diag.Diagnostic.
// That interface's Severity() and Equal() mention framework types, so satisfying it
// would pull the framework into the CLI's dependency set. The provider keeps a
// ten-line adapter instead.
type Problem interface {
	error
	Summary() string
	Detail() string
}

type problem struct {
	summary string
	detail  string
	cause   error
}

func (p problem) Summary() string { return p.summary }
func (p problem) Detail() string  { return p.detail }
func (p problem) Unwrap() error   { return p.cause }
func (p problem) Error() string {
	if p.detail == "" {
		return p.summary
	}
	return p.summary + ": " + p.detail
}

// Errorf builds a Problem. The summary is a title, so keep it short; the detail is
// where the sentence naming the flag or the environment variable belongs.
func Errorf(summary, detailFormat string, args ...any) Problem {
	return problem{summary: summary, detail: fmt.Sprintf(detailFormat, args...)}
}

// Wrap builds a Problem that keeps cause reachable through errors.Is and errors.As,
// so a caller can still match on client.HttpError underneath.
func Wrap(cause error, summary, detailFormat string, args ...any) Problem {
	return problem{summary: summary, detail: fmt.Sprintf(detailFormat, args...), cause: cause}
}
