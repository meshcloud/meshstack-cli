package internal_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/meshcloud/meshstack-cli/cmd/internal"
)

// The root command runs every command under DefaultTimeout, so a command can only take longer if
// its own deadline replaces the root's rather than being capped by it. RunPaged relies on the same.
func TestRunWithReplacesTheDeadlineOfItsCaller(t *testing.T) {
	root, cancel := context.WithTimeout(t.Context(), time.Millisecond)
	defer cancel()

	err := internal.RunWith(root, time.Hour, func(ctx context.Context) error {
		deadline, ok := ctx.Deadline()
		require.True(t, ok)
		assert.Greater(t, time.Until(deadline), time.Minute)
		<-root.Done()
		return ctx.Err()
	})

	require.NoError(t, err, "the root's deadline passed while the command still ran")
}
