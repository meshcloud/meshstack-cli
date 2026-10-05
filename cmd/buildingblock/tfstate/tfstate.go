package tfstate

import (
	"context"
	"errors"
	"fmt"
	"uuid"

	"github.com/spf13/cobra"

	"github.com/meshcloud/meshstack-cli/client"
	"github.com/meshcloud/meshstack-cli/cmd/internal"
	"github.com/meshcloud/meshstack-cli/internal/tfstate"
	"github.com/meshcloud/meshstack-cli/pkg/setting"
)

const rights = `meshStack gives the state to a login that has the rights of one of these rows in the
workspace it works in:

  Workspace the login works in            To read                To write (--mode readwrite)
  the building block's workspace          TFSTATE_LIST           TFSTATE_SAVE
  the workspace owning its definition     MANAGED_TFSTATE_LIST   MANAGED_TFSTATE_SAVE
  the admin workspace                     ADM_TFSTATE_LIST       ADM_TFSTATE_SAVE

The roles Workspace Owner and Workspace Manager have the rights of the second row, so a browser login
works where its workspace owns the definition of the building block. Any other row takes an API key
with these rights, meshstack login --apikey. Locking the state, and force-unlock, take the right to
write it. To delete the state, the login needs TFSTATE_DELETE, MANAGED_TFSTATE_DELETE or
ADM_TFSTATE_DELETE in the same row.`

const backendFile = `terraform {
    backend "http" {}
  }`

func New() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "tfstate",
		Short: "Read and write the OpenTofu state meshStack keeps for a building block",
		Long: `Read and write the OpenTofu state that meshStack keeps for a building block, which its runner
reads and writes through tofu's http backend.

` + rights + `

"meshstack buildingblock tfstate exec" serves the state to tofu's http backend, so the module needs
a backend "http" block. The runner adds one only while it runs, so add the file meshstack_backend.tf
to the module:

  ` + backendFile + `

and run "meshstack buildingblock tfstate exec <building-block-uuid> -- tofu init" once.`,
		Example: `  meshstack bb tfstate show 0b5c1d3e-5f1a-4c2b-9d7e-2a6f8e4b1c90 | jq .resources
  meshstack bb tfstate exec 0b5c1d3e-5f1a-4c2b-9d7e-2a6f8e4b1c90 -- tofu plan
  meshstack bb tfstate exec 0b5c1d3e-5f1a-4c2b-9d7e-2a6f8e4b1c90 --mode readwrite -- tofu apply
  meshstack bb tfstate force-unlock 0b5c1d3e-5f1a-4c2b-9d7e-2a6f8e4b1c90`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}

	cmd.AddCommand(newShow())
	cmd.AddCommand(newExec())
	cmd.AddCommand(newForceUnlock())

	return cmd
}

// openStore ignores the profile's default workspace. Otherwise that workspace would replace the
// building block's own workspace, which the runner stores the state under, for every user who has a
// default.
func openStore(ctx context.Context, meshStack client.Client, buildingBlockUuid uuid.UUID) (tfstate.Store, error) {
	workspace, err := setting.ResolveWorkspace(ctx, internal.SettingSources())
	if err != nil {
		return tfstate.Store{}, err
	}
	return tfstate.OpenStore(ctx, meshStack.Raw, workspace, buildingBlockUuid)
}

func withRightsHint(err error) error {
	if httpErr, ok := errors.AsType[client.HttpError](err); ok && httpErr.IsForbidden() {
		return fmt.Errorf("%w\n\n%s", err, rights)
	}
	return err
}
