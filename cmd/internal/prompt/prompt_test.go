package prompt

import (
	"bytes"
	"context"
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
	require.False(t, p.usesTerminal(), "a strings.Reader is no terminal")

	p.input.terminal = true
	copied := p
	require.True(t, copied.usesTerminal(), "a terminal is used until a line is read")

	_, err := p.Next(t.Context(), "secret")
	require.NoError(t, err)

	assert.False(t, p.usesTerminal())
	assert.False(t, copied.usesTerminal(), "a copy of the prompt has read that line as well")
	_, err = Select(t.Context(), copied, "thing", []candidate{"a", "b"}, nil)
	require.ErrorIs(t, err, ErrEndOfInput)
	assert.Equal(t, "  [1] label of a\n  [2] label of b\nSelect a thing [1-2]: ", asked.String(), "the selection asked line by line")
}
