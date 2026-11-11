package client

import (
	"encoding/json/jsontext"
	"errors"
	"fmt"
	"slices"

	"github.com/meshcloud/meshstack-cli/client/types"
	"github.com/meshcloud/meshstack-cli/client/types/enum"
	"github.com/meshcloud/meshstack-cli/client/types/hal"
	"github.com/meshcloud/meshstack-cli/internal/json"
)

const buildingBlockV2ApiVersion = "v2-preview"

type BuildingBlockHealthStatus string

var (
	BuildingBlockHealthStatuses        = enum.Enum[BuildingBlockHealthStatus]{}
	BuildingBlockHealthStatusNone      = BuildingBlockHealthStatuses.Entry("NONE")
	BuildingBlockHealthStatusSucceeded = BuildingBlockHealthStatuses.Entry("SUCCEEDED")
	BuildingBlockHealthStatusFailed    = BuildingBlockHealthStatuses.Entry("FAILED")
	BuildingBlockHealthStatusDeleted   = BuildingBlockHealthStatuses.Entry("DELETED")
)

type BuildingBlockAction string

var (
	BuildingBlockActions                    = enum.Enum[BuildingBlockAction]{}
	BuildingBlockActionProvideInput         = BuildingBlockActions.Entry("PROVIDE_INPUT")
	BuildingBlockActionWaitingForDependency = BuildingBlockActions.Entry("WAITING_FOR_DEPENDENCY")
	BuildingBlockActionApproveRun           = BuildingBlockActions.Entry("APPROVE_RUN")
	BuildingBlockActionReviewDrift          = BuildingBlockActions.Entry("REVIEW_DRIFT")
	BuildingBlockActionPending              = BuildingBlockActions.Entry("PENDING")
	BuildingBlockActionInProgress           = BuildingBlockActions.Entry("IN_PROGRESS")
	BuildingBlockActionUpgradeAvailable     = BuildingBlockActions.Entry("UPGRADE_AVAILABLE")
	BuildingBlockActionReviewErrors         = BuildingBlockActions.Entry("REVIEW_ERRORS")
	BuildingBlockActionReviewAbortedRun     = BuildingBlockActions.Entry("REVIEW_ABORTED_RUN")
	BuildingBlockActionNoAction             = BuildingBlockActions.Entry("NO_ACTION")
)

type MeshBuildingBlockV2Metadata = MeshObjectByUuidMetadata

//nolint:recvcheck // a nil block is one that is gone, and meshObjectId reads one that is there
type MeshBuildingBlockV2 struct {
	Metadata MeshBuildingBlockV2Metadata `json:"metadata" tfsdk:"metadata"`
	Spec     MeshBuildingBlockV2Spec     `json:"spec" tfsdk:"spec"`
	Status   *MeshBuildingBlockV2Status  `json:"status" tfsdk:"status"`
	Links    hal.Links                   `json:"_links,omitzero" tfsdk:"-"`
}

type MeshBuildingBlockV2Spec struct {
	BuildingBlockDefinitionVersionRef MeshBuildingBlockV2DefinitionVersionRef `json:"buildingBlockDefinitionVersionRef" tfsdk:"building_block_definition_version_ref"`
	TargetRef                         MeshBuildingBlockV2TargetRef            `json:"targetRef" tfsdk:"target_ref"`
	DisplayName                       string                                  `json:"displayName" tfsdk:"display_name"`

	// Inputs as pointer MeshBuildingBlockInput to support mocking secret responses.
	Inputs                  map[string]*MeshBuildingBlockInput `json:"inputs" tfsdk:"inputs"`
	ParentBuildingBlockRefs types.Set[UuidRef]                 `json:"parentBuildingBlockRefs" tfsdk:"parent_building_block_refs"`
}

type MeshBuildingBlockInput struct {
	Value                types.SecretOrAny                                `json:"value" tfsdk:"value"`
	ValueType            *enum.Entry[MeshBuildingBlockIOType]             `json:"valueType,omitzero" tfsdk:"-"`
	AssignmentType       enum.Entry[MeshBuildingBlockInputAssignmentType] `json:"assignmentType,omitempty" tfsdk:"-"`
	UpdateableByConsumer bool                                             `json:"updateableByConsumer" tfsdk:"-"`
	IsOptional           bool                                             `json:"isOptional" tfsdk:"-"`

	// IsSensitive decides the case of Value: [types.Secret] when true, [types.Any] otherwise.
	// [types.Variant] decodes into X first, so UnmarshalJSONFrom moves a non-sensitive value that also
	// decodes as a Secret over to Y.
	IsSensitive bool `json:"isSensitive" tfsdk:"-"`
}

func (m *MeshBuildingBlockInput) UnmarshalJSONFrom(dec *jsontext.Decoder) error {
	type wrapped MeshBuildingBlockInput
	var target wrapped
	if err := json.UnmarshalDecode(dec, &target, dec.Options()); err != nil {
		return err
	}
	*m = MeshBuildingBlockInput(target)
	switch {
	case !m.IsSensitive:
		var errs []error
		moveXtoYIfPresent := func(v *types.SecretOrAny) {
			if v.HasX() {
				xJson, err := json.Marshal(v.X)
				errs = append(errs, err)
				v.X = types.Secret{}
				errs = append(errs, json.Unmarshal(xJson, &v.Y))
			}
		}
		moveXtoYIfPresent(&m.Value)
		return errors.Join(errs...)
	case m.Value.HasY():
		return errors.New("got sensitive argument or default_value but variant Y is set instead")
	default:
		return nil
	}
}

type MeshBuildingBlockV2DefinitionVersionRef struct {
	UuidRef

	// ContentHash never reaches the backend. A Terraform config changes it to ask for a rerun when
	// the content of the referenced version changed but its uuid did not; see rerunNeeded in
	// terraform-provider-meshstack.
	ContentHash *string `json:"-" tfsdk:"content_hash"`
}

type MeshBuildingBlockV2TargetRef struct {
	Kind string  `json:"kind" tfsdk:"kind"`
	Uuid *string `json:"uuid,omitzero" tfsdk:"uuid"`
	Name *string `json:"name,omitzero" tfsdk:"name"`
}

type MeshBuildingBlockV2Status struct {
	HealthStatus enum.Entry[BuildingBlockHealthStatus] `json:"healthStatus" tfsdk:"health_status"`
	Action       enum.Entry[BuildingBlockAction]       `json:"action" tfsdk:"action"`
	Outputs      map[string]MeshBuildingBlockOutput    `json:"outputs" tfsdk:"outputs"`
	ForcePurge   bool                                  `json:"forcePurge" tfsdk:"force_purge"`
	Lifecycle    MeshLifecycle                         `json:"lifecycle" tfsdk:"-"`
	// LatestRunUuid tracks the latest *modifying* (apply/destroy) run and excludes dry runs. It is nil
	// only when no such run exists: run_transparency gates reading the run and its system messages, not
	// this identifier (MeshBuildingBlockV2RepresentationModelAssembler.resolveLatestRunUuid).
	LatestRunUuid *string `json:"latestRunUuid" tfsdk:"latest_run_uuid"`
	// LatestDryRunUuid is the latest dry (DETECT) run, but only when it is the newest run; nil otherwise.
	// Ungated like LatestRunUuid.
	LatestDryRunUuid    *string `json:"latestDryRunUuid" tfsdk:"latest_dry_run_uuid"`
	BasedOnDraftVersion bool    `json:"basedOnDraftVersion" tfsdk:"-"`
	// RunStartFailure says why meshStack could not start the run it was last asked for, phrased for the
	// user, and is "" while nothing stands in the way of a run. meshStack records it, and stamps
	// RunStartFailedOn anew, each time a request for a run fails to start one.
	RunStartFailure  string `json:"runStartFailure" tfsdk:"-"`
	RunStartFailedOn string `json:"runStartFailedOn" tfsdk:"-"`
}

// GetLatestRunUuid is "" where no modifying run exists. LatestRunUuid stays a pointer, because the
// Terraform provider writes a nil one to its state as null.
func (status MeshBuildingBlockV2Status) GetLatestRunUuid() string {
	if status.LatestRunUuid == nil {
		return ""
	}
	return *status.LatestRunUuid
}

func (status MeshBuildingBlockV2Status) GetLatestDryRunUuid() string {
	if status.LatestDryRunUuid == nil {
		return ""
	}
	return *status.LatestDryRunUuid
}

type MeshBuildingBlockOutput struct {
	Value          types.Any                                                   `json:"value" tfsdk:"value"`
	ValueType      enum.Entry[MeshBuildingBlockIOType]                         `json:"valueType" tfsdk:"value_type"`
	AssignmentType enum.Entry[MeshBuildingBlockDefinitionOutputAssignmentType] `json:"assignmentType" tfsdk:"assignment_type"`
}

// MeshBuildingBlockV2Filter never matches a soft-deleted building block: the endpoint lists
// only active ones. The json tags are the query parameter names and must match the @RequestParam
// names of fetchBuildingBlocksV2 in the meshStack backend, which ignores an unknown parameter, so
// a typo turns the filter off without an error.
type MeshBuildingBlockV2Filter struct {
	WorkspaceIdentifier *string `json:"workspaceIdentifier"`
	ProjectIdentifier   *string `json:"projectIdentifier"`
	PlatformIdentifier  *string `json:"platformIdentifier"`
	Name                *string `json:"name"`
	DefinitionName      *string `json:"definitionName"`
	DefinitionUuid      *string `json:"definitionUuid"`
	VersionUuid         *string `json:"versionUuid"`
	// VersionNumber matches version 1 for both "v1" and "1".
	VersionNumber *string `json:"versionNumber"`
	TenantUuid    *string `json:"tenantUuid"`
	// TargetKind is meshTenant or meshWorkspace.
	TargetKind *string `json:"targetRefKind"`
	// HealthStatus and Action match a building block that has any one of the values.
	HealthStatus []string `json:"healthStatus"`
	Action       []string `json:"action"`
	// ManagedByWorkspaceIdentifier and ManagedByDefinitionUuid list the building blocks of the
	// definitions a platform operator owns, and need the MANAGED_BUILDINGBLOCK_LIST permission.
	ManagedByWorkspaceIdentifier *string `json:"managedByWorkspaceIdentifier"`
	ManagedByDefinitionUuid      *string `json:"managedByDefinitionUuid"`
}

type MeshBuildingBlockV2Client = MeshObjectClient[MeshBuildingBlockV2, MeshBuildingBlockV2Filter]

type MeshBuildingBlockV2RunTrigger struct {
	DryRun bool `json:"dryRun"`
}

var (
	buildingBlockActionsRunning         = []enum.Entry[BuildingBlockAction]{BuildingBlockActionPending, BuildingBlockActionInProgress}
	buildingBlockActionsWaitingForInput = []enum.Entry[BuildingBlockAction]{BuildingBlockActionProvideInput, BuildingBlockActionWaitingForDependency, BuildingBlockActionApproveRun}
	buildingBlockActionsRunFailed       = []enum.Entry[BuildingBlockAction]{BuildingBlockActionReviewErrors, BuildingBlockActionReviewAbortedRun}
)

func (status MeshBuildingBlockV2Status) IsRunning() bool {
	return slices.Contains(buildingBlockActionsRunning, status.Action)
}

func (status MeshBuildingBlockV2Status) IsWaitingForInput() bool {
	return slices.Contains(buildingBlockActionsWaitingForInput, status.Action)
}

func (status MeshBuildingBlockV2Status) RunFailed() bool {
	return slices.Contains(buildingBlockActionsRunFailed, status.Action)
}

// RunChangedSince says whether meshStack has reacted to a write since before, by the rules of
// https://docs.meshcloud.io/concepts/building-block/#waiting-for-a-run. A zero before is a block that had no status yet.
func (status MeshBuildingBlockV2Status) RunChangedSince(before MeshBuildingBlockV2Status) bool {
	switch {
	case status.GetLatestRunUuid() != before.GetLatestRunUuid(),
		status.GetLatestDryRunUuid() != before.GetLatestDryRunUuid(),
		status.RunStartFailedOn != "" && status.RunStartFailedOn != before.RunStartFailedOn:
		return true
	case before.RunStartFailedOn != "" && status.RunStartFailedOn == "":
		// A build that includes the block again only clears the old failure.
		return false
	default:
		return !before.IsRunning() && status.actionOfRun() != before.actionOfRun()
	}
}

// actionOfRun ignores UPGRADE_AVAILABLE, which a newly released definition version sets without any run.
func (status MeshBuildingBlockV2Status) actionOfRun() enum.Entry[BuildingBlockAction] {
	if status.Action == BuildingBlockActionUpgradeAvailable {
		return BuildingBlockActionNoAction
	}
	return status.Action
}

func (bb *MeshBuildingBlockV2) IsWaitingForInput() bool {
	return bb.Status != nil && bb.Status.IsWaitingForInput()
}

// RunEndedSince is done on every action that is not running, a failed or an unknown one included:
// the caller judges the outcome.
func (bb *MeshBuildingBlockV2) RunEndedSince(before MeshBuildingBlockV2Status) (done bool, err error) {
	switch {
	case bb == nil:
		err = errors.New("building block not found")
	case bb.Status == nil, !bb.Status.RunChangedSince(before), bb.Status.IsRunning():
	default:
		done = true
	}
	return
}

// DeletionSuccessful takes the status the block had before the deletion, because meshStack reports the
// action of the run before until the delete run starts.
func (bb *MeshBuildingBlockV2) DeletionSuccessful(before MeshBuildingBlockV2Status) (done bool, err error) {
	switch {
	case bb == nil:
		// A 404: the block is gone, for example together with its definition.
		done = true
	case bb.Status != nil && bb.Status.Lifecycle.State == MeshLifecycleStateDeleted:
		// A soft-deleted block does not return 404: the backend keeps returning it with DELETED.
		// MARKED_FOR_DELETION means the deletion still runs, so polling goes on.
		done = true
	case bb.Status == nil, !bb.Status.RunChangedSince(before):
	case bb.Status.ForcePurge:
		// A force purge (definition deletion_mode PURGE, or an admin purge) deletes the block whatever
		// its delete run reports, and the lifecycle still reaches DELETED.
	case bb.Status.RunFailed():
		err = fmt.Errorf("the delete run of building block %s ended with action %s. For more details, check the building block run logs in meshStack", bb.uuidOrUnknown(), bb.Status.Action)
	case bb.Status.IsWaitingForInput():
		err = fmt.Errorf("the delete run of building block %s waits with action %s, and goes on only once someone acts on it in meshStack", bb.uuidOrUnknown(), bb.Status.Action)
	}
	return
}

func (bb MeshBuildingBlockV2) meshObjectId() string {
	return bb.Metadata.meshObjectId()
}

func (bb *MeshBuildingBlockV2) uuidOrUnknown() string {
	if bb != nil && bb.Metadata.Uuid != nil {
		return *bb.Metadata.Uuid
	}
	return "<unknown>"
}
