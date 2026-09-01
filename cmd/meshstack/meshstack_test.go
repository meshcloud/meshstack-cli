package main

import (
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// `meshstack login` and `meshstack auth login` have to be two command values. Cobra stores
// one parent per command, so registering the same value twice would silently re-parent it
// and both paths would then print the same usage line.
func TestLoginIsRegisteredTwiceAsTwoCommands(t *testing.T) {
	root := newRootCommand()

	shortcut := child(t, root, "login")
	nested := child(t, child(t, root, "auth"), "login")

	assert.NotSame(t, shortcut, nested)
	assert.Equal(t, "meshstack login", shortcut.CommandPath())
	assert.Equal(t, "meshstack auth login", nested.CommandPath())
}

func TestRootRegistersTheWholeTree(t *testing.T) {
	root := newRootCommand()

	for _, path := range [][]string{
		{"auth", "login"}, {"auth", "status"}, {"auth", "logout"},
		{"workspace", "list"},
		{"profile", "view"}, {"profile", "set"},
		{"login"},
	} {
		cmd := root
		for _, name := range path {
			cmd = child(t, cmd, name)
		}
	}

	for _, flag := range []string{"profile", "endpoint", "workspace", "no-input", "debug"} {
		assert.NotNil(t, root.PersistentFlags().Lookup(flag), "root should carry --%s", flag)
	}
}

func child(t *testing.T, parent *cobra.Command, name string) *cobra.Command {
	t.Helper()
	for _, candidate := range parent.Commands() {
		if candidate.Name() == name {
			return candidate
		}
	}
	require.Failf(t, "missing command", "%s has no child %q", parent.CommandPath(), name)
	return nil
}
