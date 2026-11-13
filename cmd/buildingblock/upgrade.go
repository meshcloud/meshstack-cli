package buildingblock

import (
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"fmt"
	"log/slog"
	"strings"
	"uuid"

	"github.com/spf13/cobra"

	"github.com/meshcloud/meshstack-cli/client"
	"github.com/meshcloud/meshstack-cli/cmd/internal"
)

const (
	versionFlagName = "version"
	inputFlagName   = "input"
)

func newUpgrade() *cobra.Command {
	var (
		output            internal.OutputFlag
		buildingBlockUuid uuid.UUID
		versionUuid       uuid.UUID
		inputs            []string
		wait              bool
	)

	cmd := &cobra.Command{
		Use:   "upgrade <building-block-uuid>",
		Short: "Upgrade a building block to another version of its definition",
		Long: `Upgrade a building block to another version of its definition, by default to the latest released version. Give the building block by its uuid.

The building block keeps the values of its inputs. Give a value for an input that the new version adds, such as a payment method, or for an input that you want to change, with --` + inputFlagName + ` <name>=<value>. The command sends a value that is valid JSON as that JSON, and any other value as a string. A payment method input takes the payment method's reference, such as {"kind": "meshPaymentMethod", "name": "<identifier>"}, and a sensitive input takes {"plaintext": "<value>"}.

meshStack upgrades a building block only to the latest released version of its definition. Only for a building block in the workspace that owns the definition, --` + versionFlagName + ` can name a draft version, to test it. Changing the version needs admin rights or the right of the workspace that owns the definition to manage its building blocks.

The command prints meshStack's response, the building block before the run that the upgrade starts. With --` + waitFlagName + `, it waits until that run has ended. This is the same as piping the response into 'meshstack buildingblock wait <uuid> --` + beforeFlagName + `=-'.`,
		Example: `  meshstack buildingblock upgrade 0b5c1d3e-5f1a-4c2b-9d7e-2a6f8e4b1c90 --wait
  meshstack bb upgrade 0b5c1d3e-5f1a-4c2b-9d7e-2a6f8e4b1c90 --input 'payment_method={"kind": "meshPaymentMethod", "name": "my-payment-method"}'`,
		Args: internal.UuidArg(&buildingBlockUuid),
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			values, err := parseInputs(inputs)
			if err != nil {
				return err
			}
			meshStack, err := internal.ResolveClient(ctx)
			if err != nil {
				return err
			}
			read, err := meshStack.Raw.Get[client.MeshBuildingBlockV2](ctx, buildingBlockUuid)
			if err != nil {
				return err
			}
			var upgrade upgradeRequest
			if err = json.Unmarshal(read, &upgrade); err != nil {
				return err
			}
			current := upgrade.Spec.BuildingBlockDefinitionVersionRef.Uuid
			if versionUuid == uuid.Nil() {
				if versionUuid, err = latestReleasedVersion(ctx, meshStack.Raw, current); err != nil {
					return err
				}
			}
			if versionUuid.String() == current {
				return fmt.Errorf("building block %s already uses version %s", buildingBlockUuid, current)
			}
			upgrade.Spec.BuildingBlockDefinitionVersionRef.Uuid = versionUuid.String()
			upgrade.Spec.Inputs = values
			payload, err := json.Marshal(upgrade)
			if err != nil {
				return err
			}
			answer, err := meshStack.Raw.Put[client.MeshBuildingBlockV2](ctx, buildingBlockUuid, payload)
			if err != nil {
				return err
			}
			slog.InfoContext(ctx, fmt.Sprintf("Asked meshStack to upgrade building block %s from version %s to version %s.", buildingBlockUuid, current, versionUuid))
			if wait {
				var accepted struct {
					Status client.MeshBuildingBlockV2Status `json:"status"`
				}
				if err = json.Unmarshal(answer, &accepted); err != nil {
					return err
				}
				return awaitRunEnd(ctx, cmd.OutOrStdout(), output.Format, meshStack.Raw, buildingBlockUuid, &accepted.Status)
			}
			return output.Format.WriteItem(cmd.OutOrStdout(), answer)
		},
	}

	output.RegisterForItem(cmd.Flags())
	cmd.Flags().Var((*internal.UuidFlag)(&versionUuid), versionFlagName, "upgrade to the version with this uuid instead of the latest released version")
	cmd.Flags().StringArrayVar(&inputs, inputFlagName, nil, "set the input to the value, given as <name>=<value>; repeat it for each input")
	cmd.Flags().BoolVar(&wait, waitFlagName, false, "wait until the run has ended, and fail if the run failed or is waiting for a person or a dependency")

	return cmd
}

// upgradeRequest is what an upgrade sends back of the building block it read. It sends only the
// inputs to set, because meshStack keeps the value of every input that a version change leaves out,
// and rejects one that the building block's owner may not set.
type upgradeRequest struct {
	ApiVersion string         `json:"apiVersion"`
	Kind       string         `json:"kind"`
	Metadata   jsontext.Value `json:"metadata"`
	Spec       struct {
		BuildingBlockDefinitionVersionRef client.UuidRef          `json:"buildingBlockDefinitionVersionRef"`
		TargetRef                         jsontext.Value          `json:"targetRef,omitzero"`
		DisplayName                       string                  `json:"displayName"`
		ParentBuildingBlockRefs           jsontext.Value          `json:"parentBuildingBlockRefs,omitzero"`
		Inputs                            map[string]upgradeInput `json:"inputs"`
	} `json:"spec"`
}

type upgradeInput struct {
	Value jsontext.Value `json:"value"`
}

func parseInputs(given []string) (map[string]upgradeInput, error) {
	values := map[string]upgradeInput{}
	for _, input := range given {
		name, value, ok := strings.Cut(input, "=")
		if !ok || name == "" {
			return nil, fmt.Errorf("--%s %q is not of the form <name>=<value>", inputFlagName, input)
		}
		raw := jsontext.Value(value)
		if !raw.IsValid() {
			var err error
			if raw, err = json.Marshal(value); err != nil {
				return nil, err
			}
		}
		values[name] = upgradeInput{Value: raw}
	}
	return values, nil
}

func latestReleasedVersion(ctx context.Context, raw *client.RawClient, currentVersion string) (uuid.UUID, error) {
	versionUuid, err := uuid.Parse(currentVersion)
	if err != nil {
		return uuid.Nil(), fmt.Errorf("the building block names its version as %q, which is no uuid", currentVersion)
	}
	read, err := raw.Get[client.MeshBuildingBlockDefinitionVersion](ctx, versionUuid)
	if err != nil {
		return uuid.Nil(), err
	}
	var version struct {
		Spec struct {
			BuildingBlockDefinitionRef *client.UuidRef `json:"buildingBlockDefinitionRef"`
		} `json:"spec"`
	}
	if err = json.Unmarshal(read, &version); err != nil {
		return uuid.Nil(), err
	}
	if version.Spec.BuildingBlockDefinitionRef == nil {
		return uuid.Nil(), fmt.Errorf("version %s names no building block definition", currentVersion)
	}
	definitionUuid, err := uuid.Parse(version.Spec.BuildingBlockDefinitionRef.Uuid)
	if err != nil {
		return uuid.Nil(), err
	}
	if read, err = raw.Get[client.MeshBuildingBlockDefinition](ctx, definitionUuid); err != nil {
		return uuid.Nil(), err
	}
	var definition struct {
		Status client.MeshBuildingBlockDefinitionStatus `json:"status"`
	}
	if err = json.Unmarshal(read, &definition); err != nil {
		return uuid.Nil(), err
	}
	if definition.Status.LatestReleasedVersionUuid == nil {
		return uuid.Nil(), fmt.Errorf("building block definition %s has no released version. Use --%s to name a draft version", definitionUuid, versionFlagName)
	}
	return uuid.Parse(*definition.Status.LatestReleasedVersionUuid)
}
