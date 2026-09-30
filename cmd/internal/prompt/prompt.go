// Package prompt asks a person on the terminal. A command passes its stdin for the answers and its
// stderr for the questions, so that stdout stays free for what the command outputs.
package prompt

import (
	"bufio"
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

const answerTime = time.Minute

// New reads in only once something asks, and then line by line for every later question, so
// several prompts of one command take their answers from the same input in turn. Each answer has to
// come within a minute, so that a script whose stdin stays open with nothing on it fails rather than
// waits forever.
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

func (p Prompt) Printf(format string, args ...any) (err error) {
	_, err = fmt.Fprintf(p.out, format, args...)
	return
}

// usesTerminal is false once Next has read a line: its reader keeps reading the input, and would
// take the keys meant for a terminal UI.
func (p Prompt) usesTerminal() bool {
	return p.input.terminal && !p.input.reading.Load()
}

// isTerminal is false for a pipe or a file: a terminal UI reads single keys in raw mode, and a
// piped "1\n" is no Enter key to it.
func isTerminal(stream any) bool {
	file, ok := stream.(*os.File)
	return ok && term.IsTerminal(file.Fd())
}
