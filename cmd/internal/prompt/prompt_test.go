package prompt

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNext(t *testing.T) {
	t.Run("an answer is read line by line", func(t *testing.T) {
		p := New(strings.NewReader(" first \nsecond\n"), io.Discard)

		for _, want := range []string{"first", "second"} {
			answer, err := p.Next(t.Context(), "thing")
			require.NoError(t, err)
			assert.Equal(t, want, answer)
		}
		_, err := p.Next(t.Context(), "thing")
		require.ErrorIs(t, err, ErrEndOfInput)
	})

	t.Run("an input that stays open with no answer fails after a minute", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			reader, writer := io.Pipe()
			defer func() { _ = writer.Close() }()
			p := New(reader, io.Discard)
			start := time.Now()

			_, err := p.Next(t.Context(), "thing")

			require.ErrorIs(t, err, context.DeadlineExceeded)
			require.EqualError(t, err, "nothing was entered for the thing: no answer came within 1m0s: context deadline exceeded")
			assert.Equal(t, time.Minute, time.Since(start))
		})
	})
}

func TestUsesTerminal(t *testing.T) {
	var asked bytes.Buffer
	p := New(strings.NewReader("\n"), &asked)
	require.False(t, p.UsesTerminal(), "a strings.Reader is no terminal")

	p.input.terminal = true
	copied := p
	require.True(t, copied.UsesTerminal(), "a terminal is used until a line is read")

	_, err := p.Next(t.Context(), "secret")
	require.NoError(t, err)

	assert.False(t, p.UsesTerminal())
	assert.False(t, copied.UsesTerminal(), "a copy of the prompt has read that line as well")
	_, err = Select(t.Context(), copied, "thing", []candidate{"a", "b"}, nil)
	require.ErrorIs(t, err, ErrEndOfInput)
	assert.Equal(t, "  [1] label of a\n  [2] label of b\nSelect a thing [1-2]: ", asked.String(), "the selection asked line by line")
}

func TestAskTakesTheDefaultForAnEmptyAnswerAndAsksAgainUntilValid(t *testing.T) {
	notEmpty := func(answer string) error {
		if answer == "" {
			return errors.New("an answer is needed")
		}
		return nil
	}
	tests := []struct {
		name          string
		defaultAnswer string
		validate      func(string) error
		answers       string
		want          string
		wantAsked     string
		wantErr       string
	}{
		{name: "an answer is taken", answers: "dev\n", want: "dev", wantAsked: "Name: "},
		{name: "an empty answer takes the default", defaultAnswer: "prod", answers: "\n", want: "prod", wantAsked: "Name [prod]: "},
		{name: "an empty answer without default is empty", answers: "\n", wantAsked: "Name: "},
		{
			name: "an answer validate refuses is asked again", validate: notEmpty, answers: "\ndev\n", want: "dev",
			wantAsked: "Name: an answer is needed\nName: ",
		},
		{name: "an input that ends takes the default", defaultAnswer: "prod", validate: notEmpty, want: "prod", wantAsked: "Name [prod]: "},
		{name: "an input that ends without default is empty", wantAsked: "Name: "},
		{
			name: "an input that ends fails where validate refuses the default", validate: notEmpty,
			wantErr: "nothing was entered for the name: the prompt reached the end of its input; an answer is needed", wantAsked: "Name: ",
		},
		{
			name: "an input that ends after a refused answer fails", validate: notEmpty, answers: "\n",
			wantErr: "nothing was entered for the name: the prompt reached the end of its input; an answer is needed", wantAsked: "Name: an answer is needed\nName: ",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var asked bytes.Buffer

			answer, err := New(strings.NewReader(tt.answers), &asked).Ask(t.Context(), "Name", tt.defaultAnswer, tt.validate)

			if tt.wantErr != "" {
				require.ErrorIs(t, err, ErrEndOfInput)
				require.EqualError(t, err, tt.wantErr)
			} else {
				require.NoError(t, err)
			}
			assert.Equal(t, tt.want, answer)
			assert.Equal(t, tt.wantAsked, asked.String())
		})
	}
}
