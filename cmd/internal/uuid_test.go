package internal_test

import (
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/meshcloud/meshstack-cli/cmd/internal"
)

func TestAUuidFlagFailsWhileTheFlagsAreParsed(t *testing.T) {
	var id string
	flags := pflag.NewFlagSet("test", pflag.ContinueOnError)
	flags.Var((*internal.UuidFlag)(&id), "definition", "")

	require.EqualError(t, flags.Parse([]string{"--definition", "my-definition"}),
		`invalid argument "my-definition" for "--definition" flag: invalid uuid`)
	require.NoError(t, flags.Parse([]string{"--definition", "3c9e1f7a-2b4d-4e6f-8a1c-5d7b9e2f4a6c"}))
	assert.Equal(t, "3c9e1f7a-2b4d-4e6f-8a1c-5d7b9e2f4a6c", id)
}

func TestAnUnsetUuidFlagShowsNoDefault(t *testing.T) {
	flags := pflag.NewFlagSet("test", pflag.ContinueOnError)
	flags.Var(new(internal.UuidFlag), "definition", "the definition")

	assert.Equal(t, "      --definition uuid   the definition\n", flags.FlagUsages())
}

func TestUuidArgChecksTheOnlyArgument(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		wantErr string
	}{
		{name: "a uuid", args: []string{"7f3a2b1c-8d4e-4f6a-9b0c-1d2e3f4a5b6c"}},
		{name: "no uuid", args: []string{"my-run"}, wantErr: `invalid argument "my-run": invalid uuid`},
		{name: "two arguments", args: []string{"7f3a2b1c-8d4e-4f6a-9b0c-1d2e3f4a5b6c", "more"}, wantErr: "accepts 1 arg(s), received 2"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := internal.UuidArg(&cobra.Command{}, tt.args)
			if tt.wantErr != "" {
				assert.EqualError(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
		})
	}
}
