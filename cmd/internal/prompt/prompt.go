// Package prompt asks a person on the terminal. A command passes its stdin for the answers and its
// stderr for the questions, so that stdout stays free for what the command outputs.
package prompt

import (
	"bufio"
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/charmbracelet/x/term"
)

// ErrEndOfInput is what Next returns once the input has ended, which a caller can accept as no
// answer at all where one is optional.
var ErrEndOfInput = errors.New("the prompt reached the end of its input")

type Prompt struct {
	input *input
	out   io.Writer
}

// input is shared by every copy of a Prompt, so that all of them agree on whether its lines are
// being read yet.
type input struct {
	in       io.Reader
	terminal bool
	reading  atomic.Bool
	lines    func() <-chan string
}

// answerTime makes a script whose stdin stays open with nothing on it fail rather than wait
// forever.
const answerTime = time.Minute

// New reads in only once something asks, and then line by line for every later question, so
// several prompts of one command take their answers from the same input in turn.
func New(in io.Reader, out io.Writer) Prompt {
	return Prompt{
		input: &input{
			in:       in,
			terminal: isTerminal(in) && isTerminal(out),
			lines: sync.OnceValue(func() <-chan string {
				lines := make(chan string)
				go func() {
					defer close(lines)
					answers := bufio.NewScanner(in)
					for answers.Scan() {
						lines <- answers.Text()
					}
				}()
				return lines
			}),
		},
		out: out,
	}
}

func (p Prompt) Next(ctx context.Context, what string) (string, error) {
	ctx, cancel := context.WithTimeoutCause(ctx, answerTime,
		fmt.Errorf("no answer came within %s: %w", answerTime, context.DeadlineExceeded))
	defer cancel()
	p.input.reading.Store(true)
	select {
	case <-ctx.Done():
		return "", fmt.Errorf("nothing was entered for the %s: %w", what, context.Cause(ctx))
	case line, open := <-p.input.lines():
		if !open {
			return "", fmt.Errorf("nothing was entered for the %s: %w", what, ErrEndOfInput)
		}
		return strings.TrimSpace(line), nil
	}
}

// Ask takes an input that ends before an answer as an empty answer, so a script can give every
// answer as a flag and leave stdin empty. It fails only where validate refuses that.
func (p Prompt) Ask(ctx context.Context, what, defaultAnswer string, validate func(string) error) (string, error) {
	question := what + ": "
	if defaultAnswer != "" {
		question = fmt.Sprintf("%s [%s]: ", what, defaultAnswer)
	}
	if validate == nil {
		validate = func(string) error { return nil }
	}
	for {
		if err := p.Printf("%s", question); err != nil {
			return "", err
		}
		answer, err := p.Next(ctx, strings.ToLower(what))
		ended := errors.Is(err, ErrEndOfInput)
		if err != nil && !ended {
			return "", err
		}
		answer = cmp.Or(answer, defaultAnswer)
		problem := validate(answer)
		switch {
		case problem == nil:
			return answer, nil
		case ended:
			return "", fmt.Errorf("%w; %w", err, problem)
		}
		if err := p.Printf("%s\n", problem); err != nil {
			return "", err
		}
	}
}

func (p Prompt) Printf(format string, args ...any) (err error) {
	_, err = fmt.Fprintf(p.out, format, args...)
	return
}

// UsesTerminal is false once Next has read a line: its reader keeps reading the input, and would
// take the keys meant for a terminal UI.
func (p Prompt) UsesTerminal() bool {
	return p.input.terminal && !p.input.reading.Load()
}

// isTerminal is false for a pipe or a file: a terminal UI reads single keys in raw mode, and a
// piped "1\n" is no Enter key to it.
func isTerminal(stream any) bool {
	file, ok := stream.(*os.File)
	return ok && term.IsTerminal(file.Fd())
}
