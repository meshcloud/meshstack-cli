package internal

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestWrapHelp(t *testing.T) {
	t.Run("a paragraph wraps at spaces only, and a blank line stays", func(t *testing.T) {
		assert.Equal(t, "use one\n--override-backend\n\nnext", wrapHelp("use one --override-backend\n\nnext\n", 12))
	})

	t.Run("a list item continues under its text, and an indented line stays as it is", func(t *testing.T) {
		assert.Equal(t, "  - one two\n    three\n    code that stays long", wrapHelp("  - one two three\n    code that stays long", 12))
	})
}
