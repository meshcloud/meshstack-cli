package buildingblock

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"log/slog"
	"uuid"

	"github.com/spf13/cobra"

	"github.com/meshcloud/meshstack-cli/client"
	"github.com/meshcloud/meshstack-cli/cmd/internal"
)

const fileFlagName = "file"

func newCreate() *cobra.Command {
	var (
		output internal.OutputFlag
		file   internal.JsonInputFlag[jsontext.Value]
		wait   bool
	)

	cmd := &cobra.Command{
		Use:   "create -f <file|->",
		Short: "Create a building block from a file",
		Long: `Create a building block from a JSON file, or from stdin with -f -. The file holds the building block as the meshStack API takes it; 'meshstack api-docs --describe buildingblock' shows its fields.

The command prints meshStack's response, the building block before its first run has started. With --` + waitFlagName + `, it waits until that run has ended.`,
		Example: `  meshstack buildingblock create -f block.json --wait
  jq -n '{kind: "meshBuildingBlock", spec: {...}}' | meshstack bb create -f - -o ndjson | jq -r .metadata.uuid`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			payload, err := file.Read(cmd)
			if err != nil {
				return err
			}
			meshStack, err := internal.ResolveClient(ctx)
			if err != nil {
				return err
			}
			answer, err := meshStack.Raw.Post[client.MeshBuildingBlockV2](ctx, payload)
			if err != nil {
				return err
			}
			var created struct {
				Metadata client.MeshBuildingBlockV2Metadata `json:"metadata"`
				Status   client.MeshBuildingBlockV2Status   `json:"status"`
			}
			if err = json.Unmarshal(answer, &created); err != nil {
				return err
			}
			if created.Metadata.Uuid == nil {
				return errors.New("meshStack created the building block, but its response names no uuid")
			}
			buildingBlockUuid, err := uuid.Parse(*created.Metadata.Uuid)
			if err != nil {
				return err
			}
			slog.InfoContext(ctx, fmt.Sprintf("Created building block %s.", buildingBlockUuid))
			if wait {
				return awaitRunEnd(ctx, cmd.OutOrStdout(), output.Format, meshStack.Raw, buildingBlockUuid, &created.Status)
			}
			return output.Format.WriteItem(cmd.OutOrStdout(), answer)
		},
	}

	output.RegisterForItem(cmd.Flags())
	file.RegisterP(cmd.Flags(), fileFlagName, "f", "file with the building block, or - to read it from stdin")
	_ = cmd.MarkFlagRequired(fileFlagName)
	cmd.Flags().BoolVar(&wait, waitFlagName, false, "wait until the run has ended, and fail if the run failed or is waiting for a person or a dependency")

	return cmd
}
