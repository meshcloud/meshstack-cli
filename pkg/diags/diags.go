// Package diags carries an error that a rich front end can render.
//
// pkg/auth returns these so that no caller has to parse a string: the meshStack CLI
// prints a summary and a paragraph of advice, and the Terraform provider turns the
// same two fields into a diagnostic. Some outcomes succeed *and* warn — picking a
// profile by endpoint is the case that forced it — which an error return cannot
// express, so a Problem also says whether it is fatal.
package diags

import (
	"errors"
	"fmt"
	"log/slog"
)

// Problem is an error that already carries the parts a rich front end wants: a title,
// an actionable paragraph, and whether it is fatal.
//
// It deliberately does not implement terraform-plugin-framework's diag.Diagnostic.
// That interface's Severity() and Equal() mention framework types, so satisfying it
// would pull the framework into the CLI's dependency set. The provider keeps a
// ten-line adapter instead.
type Problem interface {
	error
	Summary() string
	Detail() string
	IsWarning() bool
}

type problem struct {
	summary string
	detail  string
	warning bool
	cause   error
}

func (p problem) Summary() string { return p.summary }
func (p problem) Detail() string  { return p.detail }
func (p problem) IsWarning() bool { return p.warning }
func (p problem) Unwrap() error   { return p.cause }
func (p problem) Error() string {
	if p.detail == "" {
		return p.summary
	}
	return p.summary + ": " + p.detail
}

// Errorf builds a fatal Problem. The summary is a title, so keep it short; the detail
// is where the sentence naming the flag or the environment variable belongs.
func Errorf(summary, detailFormat string, args ...any) Problem {
	return problem{summary: summary, detail: fmt.Sprintf(detailFormat, args...)}
}

// Wrap builds a fatal Problem that keeps cause reachable through errors.Is and
// errors.As, so a caller can still match on client.HttpError underneath.
func Wrap(cause error, summary, detailFormat string, args ...any) Problem {
	return problem{summary: summary, detail: fmt.Sprintf(detailFormat, args...), cause: cause}
}

// Warnf builds a Problem that reports something which did not stop the work.
func Warnf(summary, detailFormat string, args ...any) Problem {
	return problem{summary: summary, detail: fmt.Sprintf(detailFormat, args...), warning: true}
}

// HandleErr turns a warning into a log record and reports it as success.
// cmd/meshstack installs it around every command, so a warning prints and the command
// still exits 0 — the same experience as a warning during `terraform apply`.
func HandleErr(err error) error {
	p, ok := errors.AsType[Problem](err)
	if !ok || !p.IsWarning() {
		return err
	}
	slog.Warn(p.Summary(), "detail", p.Detail())
	return nil
}
