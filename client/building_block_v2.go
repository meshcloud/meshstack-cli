package client

import (
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"slices"

	"github.com/meshcloud/meshstack-cli/client/internal"
	"github.com/meshcloud/meshstack-cli/client/types"
	"github.com/meshcloud/meshstack-cli/client/types/enum"
	"github.com/meshcloud/meshstack-cli/internal/http"
)

type BuildingBlockLifecycleState string

var (
	BuildingBlockLifecycleStates                 = enum.Enum[BuildingBlockLifecycleState]{}
	BuildingBlockLifecycleStateActive            = BuildingBlockLifecycleStates.Entry("ACTIVE")
	BuildingBlockLifecycleStateMarkedForDeletion = BuildingBlockLifecycleStates.Entry("MARKED_FOR_DELETION")
	BuildingBlockLifecycleStateDeleted           = BuildingBlockLifecycleStates.Entry("DELETED")
)

type BuildingBlockStatus string

var (
	BuildingBlockStatuses                       = enum.Enum[BuildingBlockStatus]{}
	BuildingBlockStatusWaitingForDependentInput = BuildingBlockStatuses.Entry("WAITING_FOR_DEPENDENT_INPUT")
	BuildingBlockStatusWaitingForOperatorInput  = BuildingBlockStatuses.Entry("WAITING_FOR_OPERATOR_INPUT")
	BuildingBlockStatusWaitingForUserInput      = BuildingBlockStatuses.Entry("WAITING_FOR_USER_INPUT")
	BuildingBlockStatusWaitingForApproval       = BuildingBlockStatuses.Entry("WAITING_FOR_APPROVAL")
	BuildingBlockStatusPending                  = BuildingBlockStatuses.Entry("PENDING")
	BuildingBlockStatusInProgress               = BuildingBlockStatuses.Entry("IN_PROGRESS")
	BuildingBlockStatusSucceeded                = BuildingBlockStatuses.Entry("SUCCEEDED")
	BuildingBlockStatusFailed                   = BuildingBlockStatuses.Entry("FAILED")
	BuildingBlockStatusAborted                  = BuildingBlockStatuses.Entry("ABORTED")
)

type MeshBuildingBlockV2 struct {
	Metadata MeshBuildingBlockV2Metadata `json:"metadata" tfsdk:"metadata"`
	Spec     MeshBuildingBlockV2Spec     `json:"spec" tfsdk:"spec"`
	Status   *MeshBuildingBlockV2Status  `json:"status" tfsdk:"status"`
}

type MeshBuildingBlockV2Metadata struct {
	Uuid             *string `json:"uuid" tfsdk:"uuid"`
	OwnedByWorkspace string  `json:"ownedByWorkspace" tfsdk:"owned_by_workspace"`
}

type MeshBuildingBlockV2Spec struct {
	BuildingBlockDefinitionVersionRef MeshBuildingBlockV2DefinitionVersionRef `json:"buildingBlockDefinitionVersionRef" tfsdk:"building_block_definition_version_ref"`
	TargetRef                         MeshBuildingBlockV2TargetRef            `json:"targetRef" tfsdk:"target_ref"`
	DisplayName                       string                                  `json:"displayName" tfsdk:"display_name"`

	// Inputs as pointer MeshBuildingBlockInput to support mocking secret responses.
	Inputs                  map[string]*MeshBuildingBlockInput `json:"inputs" tfsdk:"inputs"`
	ParentBuildingBlockRefs types.Set[UuidRef]                 `json:"parentBuildingBlockRefs" tfsdk:"parent_building_block_refs"`

	// ParentBuildingBlocks is the deprecated parentBuildingBlocks field, which MarshalJSON and
	// UnmarshalJSON carry. The deprecated meshstack_building_block_v2 surfaces of the Terraform
	// provider read the definition uuid they report from it.
	ParentBuildingBlocks types.Set[MeshBuildingBlockV2Parent] `json:"-" tfsdk:"-"`
}

// MeshBuildingBlockV2Parent is an entry of the deprecated parentBuildingBlocks field.
type MeshBuildingBlockV2Parent struct {
	UuidRef

	// BuildingBlockUuid always holds the same value as Uuid.
	BuildingBlockUuid string `json:"buildingBlockUuid"`
	// DefinitionUuid is the parent's building block definition. The backend derives it from the
	// referenced block, so every response carries it and a request never does.
	DefinitionUuid string `json:"definitionUuid,omitempty"`
}

// UnmarshalJSON fills Uuid from the deprecated buildingBlockUuid, which is where a response carries
// the parent's identity.
func (p *MeshBuildingBlockV2Parent) UnmarshalJSON(data []byte) error {
	type wire MeshBuildingBlockV2Parent
	var target wire
	if err := json.Unmarshal(data, &target); err != nil {
		return err
	}

	*p = MeshBuildingBlockV2Parent(target)
	if p.Uuid == "" {
		p.Uuid = p.BuildingBlockUuid
	}
	p.BuildingBlockUuid = p.Uuid
	if p.Kind == "" {
		p.Kind = MeshObjectKind.BuildingBlock
	}

	return nil
}

// wireCompatibility repeats the options internal/json marshals every request with: a v1-style
// MarshalJSON receives none of its caller's, so without it a nested value would go out in a
// different shape than the request around it.
var wireCompatibility = json.JoinOptions(
	json.Deterministic(true),
	json.FormatNilSliceAsNull(true),
	json.FormatNilMapAsNull(true),
)

// MarshalJSON sends the parents under both field names: parentBuildingBlockRefs, and the deprecated
// parentBuildingBlocks for a backend that does not know the new field yet. A newer backend accepts
// both as long as they name the same building blocks, and an older one ignores the field it does not
// know, because the meshObject API does not reject unknown properties.
//
// Together with UnmarshalJSON this is the whole compatibility window. Once every backend still in use
// knows parentBuildingBlockRefs, both methods can go.
func (s MeshBuildingBlockV2Spec) MarshalJSON() ([]byte, error) {
	type wire MeshBuildingBlockV2Spec
	w := wire(s)
	if len(w.ParentBuildingBlockRefs) == 0 {
		w.ParentBuildingBlockRefs = parentRefsFromDeprecated(w.ParentBuildingBlocks)
	}

	var fields map[string]jsontext.Value
	if encoded, err := json.Marshal(w, wireCompatibility); err != nil {
		return nil, err
	} else if err := json.Unmarshal(encoded, &fields); err != nil {
		return nil, err
	}

	// The deprecated entry sends only buildingBlockUuid: every backend in the supported range reads
	// the parent from it, and the definition uuid is always derived from the referenced block.
	parents := make([]struct {
		BuildingBlockUuid string `json:"buildingBlockUuid"`
	}, 0, len(s.ParentBuildingBlockRefs))
	for _, ref := range s.ParentBuildingBlockRefs {
		parents = append(parents, struct {
			BuildingBlockUuid string `json:"buildingBlockUuid"`
		}{BuildingBlockUuid: ref.Uuid})
	}
	var err error
	if fields["parentBuildingBlocks"], err = json.Marshal(parents); err != nil {
		return nil, err
	}

	return json.Marshal(fields, wireCompatibility)
}

func parentRefsFromDeprecated(parents types.Set[MeshBuildingBlockV2Parent]) types.Set[UuidRef] {
	refs := make(types.Set[UuidRef], 0, len(parents))
	for _, parent := range parents {
		refs = append(refs, UuidRef{Uuid: parent.Uuid, Kind: MeshObjectKind.BuildingBlock})
	}
	return refs
}

// UnmarshalJSON reads the parents from parentBuildingBlockRefs, or from the deprecated
// parentBuildingBlocks when a backend does not serve the new field yet. Terraform then sees the same
// elements against either backend and set hashing stays stable.
func (s *MeshBuildingBlockV2Spec) UnmarshalJSON(data []byte) error {
	type wire MeshBuildingBlockV2Spec
	var target struct {
		wire

		ParentBuildingBlocks types.Set[MeshBuildingBlockV2Parent] `json:"parentBuildingBlocks"`
	}
	if err := json.Unmarshal(data, &target); err != nil {
		return err
	}

	*s = MeshBuildingBlockV2Spec(target.wire)
	s.ParentBuildingBlocks = target.ParentBuildingBlocks
	if len(s.ParentBuildingBlockRefs) == 0 {
		s.ParentBuildingBlockRefs = parentRefsFromDeprecated(s.ParentBuildingBlocks)
	}
	for i := range s.ParentBuildingBlockRefs {
		if s.ParentBuildingBlockRefs[i].Kind == "" {
			s.ParentBuildingBlockRefs[i].Kind = MeshObjectKind.BuildingBlock
		}
	}

	return nil
}

type MeshBuildingBlockInput struct {
	Value          types.SecretOrAny                                `json:"value" tfsdk:"value"`
	ValueType      *enum.Entry[MeshBuildingBlockIOType]             `json:"valueType,omitzero" tfsdk:"-"`
	AssignmentType enum.Entry[MeshBuildingBlockInputAssignmentType] `json:"assignmentType,omitempty" tfsdk:"-"`

	// IsSensitive decides the case of Value: [types.Secret] when true, [types.Any] otherwise.
	// [types.Variant] decodes into X first, so UnmarshalJSON moves a non-sensitive value that also
	// decodes as a Secret over to Y.
	IsSensitive bool `json:"isSensitive" tfsdk:"-"`
}

func (m *MeshBuildingBlockInput) UnmarshalJSON(bytes []byte) error {
	type wrapped MeshBuildingBlockInput
	var target wrapped
	if err := json.Unmarshal(bytes, &target); err != nil {
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
	Uuid *string `json:"uuid" tfsdk:"uuid"`
	Name *string `json:"name" tfsdk:"name"`
}

type MeshBuildingBlockV2Lifecycle struct {
	State enum.Entry[BuildingBlockLifecycleState] `json:"state" tfsdk:"state"`
}

type MeshBuildingBlockV2Status struct {
	Status     enum.Entry[BuildingBlockStatus]    `json:"status" tfsdk:"status"`
	Outputs    map[string]MeshBuildingBlockOutput `json:"outputs" tfsdk:"outputs"`
	ForcePurge bool                               `json:"forcePurge" tfsdk:"force_purge"`
	Lifecycle  MeshBuildingBlockV2Lifecycle       `json:"lifecycle" tfsdk:"-"`
	// LatestRunUuid tracks the latest *modifying* (apply/destroy) run and excludes dry runs. It is nil
	// only when no such run exists: run_transparency gates reading the run and its system messages, not
	// this identifier (MeshBuildingBlockV2RepresentationModelAssembler.resolveLatestRunUuid).
	LatestRunUuid *string `json:"latestRunUuid" tfsdk:"latest_run_uuid"`
	// LatestDryRunUuid is the latest dry (DETECT) run, but only when it is the newest run; nil otherwise.
	// Ungated like LatestRunUuid.
	LatestDryRunUuid *string `json:"latestDryRunUuid" tfsdk:"latest_dry_run_uuid"`
	// RunStartFailure says why meshStack could not start the run it was last asked for, phrased for the
	// user, and is nil while nothing stands in the way of a run. meshStack records it, and stamps
	// RunStartFailedOn anew, each time a request for a run fails to start one.
	RunStartFailure  *string `json:"runStartFailure" tfsdk:"-"`
	RunStartFailedOn *string `json:"runStartFailedOn" tfsdk:"-"`
}

type MeshBuildingBlockOutput struct {
	Value          types.Any                                                   `json:"value" tfsdk:"value"`
	ValueType      enum.Entry[MeshBuildingBlockIOType]                         `json:"valueType" tfsdk:"value_type"`
	AssignmentType enum.Entry[MeshBuildingBlockDefinitionOutputAssignmentType] `json:"assignmentType" tfsdk:"assignment_type"`
}

// MeshBuildingBlockV2ListFilter never matches a soft-deleted building block: the endpoint lists
// only active ones. The json tags are the query parameter names and must match the @RequestParam
// names of fetchBuildingBlocksV2 in the meshStack backend, which ignores an unknown parameter, so
// a typo turns the filter off without an error.
type MeshBuildingBlockV2ListFilter struct {
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
	Status     *string `json:"status"`
	// HealthStatus and Action match a building block that has any one of the values.
	HealthStatus []string `json:"healthStatus"`
	Action       []string `json:"action"`
	// ManagedByWorkspaceIdentifier and ManagedByDefinitionUuid list the building blocks of the
	// definitions a platform operator owns, and need the MANAGED_BUILDINGBLOCK_LIST permission.
	ManagedByWorkspaceIdentifier *string      `json:"managedByWorkspaceIdentifier"`
	ManagedByDefinitionUuid      *string      `json:"managedByDefinitionUuid"`
	Sort                         SortCriteria `json:"sort"`
}

type MeshBuildingBlockV2Client interface {
	Read(ctx context.Context, uuid string) (*MeshBuildingBlockV2, error)
	ReadFunc(uuid string) func(ctx context.Context) (*MeshBuildingBlockV2, error)
	List(ctx context.Context, filter MeshBuildingBlockV2ListFilter) ([]MeshBuildingBlockV2, error)
	Create(ctx context.Context, bb *MeshBuildingBlockV2) (*MeshBuildingBlockV2, error)
	Update(ctx context.Context, bb *MeshBuildingBlockV2) (*MeshBuildingBlockV2, error)
	Delete(ctx context.Context, uuid string, purge bool) error
	TriggerRun(ctx context.Context, uuid string) (*MeshBuildingBlockV2, error)
}

type meshBuildingBlockV2Client struct {
	meshObject internal.MeshObjectClient[MeshBuildingBlockV2]
}

func newBuildingBlockV2Client(ctx context.Context, httpClient internal.HttpClient) meshBuildingBlockV2Client {
	return meshBuildingBlockV2Client{internal.NewMeshObjectClient[MeshBuildingBlockV2](ctx, httpClient, "v2-preview")}
}

func (c meshBuildingBlockV2Client) Read(ctx context.Context, uuid string) (*MeshBuildingBlockV2, error) {
	return c.ReadFunc(uuid)(ctx)
}

func (c meshBuildingBlockV2Client) ReadFunc(uuid string) func(ctx context.Context) (*MeshBuildingBlockV2, error) {
	return func(ctx context.Context) (*MeshBuildingBlockV2, error) {
		return c.meshObject.Get(ctx, uuid)
	}
}

func (c meshBuildingBlockV2Client) List(ctx context.Context, filter MeshBuildingBlockV2ListFilter) ([]MeshBuildingBlockV2, error) {
	return c.meshObject.List(ctx, http.WithUrlQuery(filter))
}

func (c meshBuildingBlockV2Client) Create(ctx context.Context, bb *MeshBuildingBlockV2) (*MeshBuildingBlockV2, error) {
	return c.meshObject.Post(ctx, bb)
}

func (c meshBuildingBlockV2Client) Update(ctx context.Context, bb *MeshBuildingBlockV2) (*MeshBuildingBlockV2, error) {
	if bb.Metadata.Uuid == nil {
		return nil, errors.New("cannot update building block without UUID")
	}
	return c.meshObject.Put(ctx, *bb.Metadata.Uuid, bb)
}

func (c meshBuildingBlockV2Client) Delete(ctx context.Context, uuid string, purge bool) error {
	if purge {
		return c.meshObject.DeleteAtPath(ctx, uuid, "purge")
	}
	return c.meshObject.Delete(ctx, uuid)
}

func (bb *MeshBuildingBlockV2) IsWaitingForInput() bool {
	return bb.Status.Status == BuildingBlockStatusWaitingForOperatorInput ||
		bb.Status.Status == BuildingBlockStatusWaitingForUserInput ||
		bb.Status.Status == BuildingBlockStatusWaitingForDependentInput ||
		bb.Status.Status == BuildingBlockStatusWaitingForApproval
}

func (bb *MeshBuildingBlockV2) CreateSuccessful() (done bool, err error) {
	switch {
	case bb == nil:
		err = errors.New("building block not found after creation")
	case bb.Status == nil:
		// keep polling
	case bb.Status.Status == BuildingBlockStatusFailed,
		bb.Status.Status == BuildingBlockStatusAborted:
		err = fmt.Errorf("building block %s reached %s state, check run logs in meshStack", bb.uuidOrUnknown(), bb.Status.Status)
	case bb.IsWaitingForInput():
		// A waiting run does not go on by itself, so stop polling and leave the warning to the caller.
		done = true
	case bb.Status.Status == BuildingBlockStatusSucceeded:
		done = true
	case !slices.Contains(BuildingBlockStatuses, bb.Status.Status):
		// Fail now: a status this client does not know would keep the poll going until it times out.
		err = fmt.Errorf("unknown building block status %q for building block %s; provider may be out of date", bb.Status.Status, bb.uuidOrUnknown())
	}
	return
}

func (bb *MeshBuildingBlockV2) DeletionSuccessful() (done bool, err error) {
	switch {
	case bb == nil:
		// A 404: the block is gone, for example together with its definition.
		done = true
	case bb.Status != nil && bb.Status.Lifecycle.State == BuildingBlockLifecycleStateDeleted:
		// A soft-deleted block does not return 404: the backend keeps returning it with DELETED.
		// MARKED_FOR_DELETION means the deletion still runs, so polling goes on.
		done = true
	case bb.Status != nil && bb.Status.Status == BuildingBlockStatusFailed:
		// A force purge (definition deletion_mode PURGE, or an admin purge) deletes the block whatever
		// its delete run reports, so FAILED passes and the lifecycle still reaches DELETED.
		if !bb.Status.ForcePurge {
			err = fmt.Errorf("building block %s reached FAILED state during deletion. For more details, check the building block run logs in meshStack", bb.uuidOrUnknown())
		}
	}
	return
}

func (bb *MeshBuildingBlockV2) uuidOrUnknown() string {
	if bb != nil && bb.Metadata.Uuid != nil {
		return *bb.Metadata.Uuid
	}
	return "<unknown>"
}

// TriggerRun answers with the building block as meshStack left it on accepting the run: Status.Status
// is PENDING, while LatestRunUuid, RunStartFailure and RunStartFailedOn still report the request
// before, because meshStack starts the run, or records why it could not, only after it answered.
func (c meshBuildingBlockV2Client) TriggerRun(ctx context.Context, bbUuid string) (*MeshBuildingBlockV2, error) {
	// dryRun is not optional to the endpoint once a body is sent, so it goes out as false rather
	// than being omitted.
	return c.meshObject.PostAtPath[*MeshBuildingBlockV2](ctx, struct {
		DryRun bool `json:"dryRun"`
	}{}, bbUuid, "trigger-run")
}
