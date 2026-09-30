// Package prompt asks a person on the terminal. A command passes its stdin for the answers and its
// stderr for the questions, so that stdout stays free for what the command outputs.
package prompt

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"
)

// ErrEndOfInput is what Next returns once the input has ended, which a caller can accept as no
// answer at all where one is optional.
var ErrEndOfInput = errors.New("the prompt reached the end of its input")

type Prompt struct {
	in  func() <-chan string
	out io.Writer
}

const answerTime = time.Minute

// New reads in only once something asks, and then line by line for every later question, so
// several prompts of one command take their answers from the same input in turn. Each answer has to
// come within a minute, so that a script whose stdin stays open with nothing on it fails rather than
// waits forever.
func New(in io.Reader, out io.Writer) Prompt {
	return Prompt{
		in: sync.OnceValue(func() <-chan string {
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
		out: out,
	}
}

func (p Prompt) Next(ctx context.Context, what string) (string, error) {
	ctx, cancel := context.WithTimeoutCause(ctx, answerTime,
		fmt.Errorf("no answer came within %s: %w", answerTime, context.DeadlineExceeded))
	defer cancel()
	select {
	case <-ctx.Done():
		return "", fmt.Errorf("nothing was entered for the %s: %w", what, context.Cause(ctx))
	case line, open := <-p.in():
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
