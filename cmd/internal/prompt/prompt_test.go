package prompt

import (
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
