package client

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"uuid"

	"github.com/meshcloud/meshstack-cli/client/internal"
	"github.com/meshcloud/meshstack-cli/client/types"
	"github.com/meshcloud/meshstack-cli/client/types/enum"
	"github.com/meshcloud/meshstack-cli/internal/http"
)

type MeshBuildingBlockDefinitionVersionState string

var (
	MeshBuildingBlockDefinitionVersionStates        = enum.Enum[MeshBuildingBlockDefinitionVersionState]{}
	MeshBuildingBlockDefinitionVersionStateDraft    = MeshBuildingBlockDefinitionVersionStates.Entry("DRAFT")
	MeshBuildingBlockDefinitionVersionStateReleased = MeshBuildingBlockDefinitionVersionStates.Entry("RELEASED")
)

type BuildingBlockDeletionMode string

var (
	BuildingBlockDeletionModes      = enum.Enum[BuildingBlockDeletionMode]{}
	BuildingBlockDeletionModeDelete = BuildingBlockDeletionModes.Entry("DELETE")
	BuildingBlockDeletionModePurge  = BuildingBlockDeletionModes.Entry("PURGE")
)

type MeshBuildingBlockIOType string

var (
	MeshBuildingBlockIOTypes            = enum.Enum[MeshBuildingBlockIOType]{}
	MeshBuildingBlockIOTypeString       = MeshBuildingBlockIOTypes.Entry("STRING")
	MeshBuildingBlockIOTypeCode         = MeshBuildingBlockIOTypes.Entry("CODE")
	MeshBuildingBlockIOTypeInteger      = MeshBuildingBlockIOTypes.Entry("INTEGER")
	MeshBuildingBlockIOTypeBoolean      = MeshBuildingBlockIOTypes.Entry("BOOLEAN")
	MeshBuildingBlockIOTypeFile         = MeshBuildingBlockIOTypes.Entry("FILE")
	MeshBuildingBlockIOTypeList         = MeshBuildingBlockIOTypes.Entry("LIST")
	MeshBuildingBlockIOTypeSingleSelect = MeshBuildingBlockIOTypes.Entry("SINGLE_SELECT")
	MeshBuildingBlockIOTypeMultiSelect  = MeshBuildingBlockIOTypes.Entry("MULTI_SELECT")

	// MeshBuildingBlockIOTypeJson is deliberately not an entry of MeshBuildingBlockIOTypes: a
	// definition input declaring it describes a form of its own, through the accompanying JsonSchema.
	// What that form produces is JSON text, which a building block's own inputs report as CODE.
	MeshBuildingBlockIOTypeJson = enum.Entry[MeshBuildingBlockIOType]("JSON")
)

var MeshBuildingBlockDefinitionInputTypes = MeshBuildingBlockIOTypes.With(MeshBuildingBlockIOTypeJson)

var MeshBuildingBlockOutputIOTypes = enum.Of(
	MeshBuildingBlockIOTypeString,
	MeshBuildingBlockIOTypeCode,
	MeshBuildingBlockIOTypeInteger,
	MeshBuildingBlockIOTypeBoolean,
)

type MeshBuildingBlockInputAssignmentType string

var (
	MeshBuildingBlockInputAssignmentTypes                           = enum.Enum[MeshBuildingBlockInputAssignmentType]{}
	MeshBuildingBlockInputAssignmentTypeAuthor                      = MeshBuildingBlockInputAssignmentTypes.Entry("AUTHOR")
	MeshBuildingBlockInputAssignmentTypeUserInput                   = MeshBuildingBlockInputAssignmentTypes.Entry("USER_INPUT")
	MeshBuildingBlockInputAssignmentTypePlatformOperatorManualInput = MeshBuildingBlockInputAssignmentTypes.Entry("PLATFORM_OPERATOR_MANUAL_INPUT")
	MeshBuildingBlockInputAssignmentTypeBuildingBlockOutput         = MeshBuildingBlockInputAssignmentTypes.Entry("BUILDING_BLOCK_OUTPUT")
	MeshBuildingBlockInputAssignmentTypePlatformTenantID            = MeshBuildingBlockInputAssignmentTypes.Entry("PLATFORM_TENANT_ID")
	MeshBuildingBlockInputAssignmentTypeMeshstackTenantUuid         = MeshBuildingBlockInputAssignmentTypes.Entry("MESHSTACK_TENANT_UUID")
	MeshBuildingBlockInputAssignmentTypeWorkspaceIdentifier         = MeshBuildingBlockInputAssignmentTypes.Entry("WORKSPACE_IDENTIFIER")
	MeshBuildingBlockInputAssignmentTypeProjectIdentifier           = MeshBuildingBlockInputAssignmentTypes.Entry("PROJECT_IDENTIFIER")
	MeshBuildingBlockInputAssignmentTypeFullPlatformIdentifier      = MeshBuildingBlockInputAssignmentTypes.Entry("FULL_PLATFORM_IDENTIFIER")
	MeshBuildingBlockInputAssignmentTypeTenantBuildingBlockUuid     = MeshBuildingBlockInputAssignmentTypes.Entry("TENANT_BUILDING_BLOCK_UUID")
	MeshBuildingBlockInputAssignmentTypeStatic                      = MeshBuildingBlockInputAssignmentTypes.Entry("STATIC")
	MeshBuildingBlockInputAssignmentTypeUserPermissions             = MeshBuildingBlockInputAssignmentTypes.Entry("USER_PERMISSIONS")
	MeshBuildingBlockInputAssignmentTypeTag                         = MeshBuildingBlockInputAssignmentTypes.Entry("TAG")
	MeshBuildingBlockInputAssignmentTypePaymentMethod               = MeshBuildingBlockInputAssignmentTypes.Entry("PAYMENT_METHOD")
)

// MeshBuildingBlockTagInputTarget names the meshObject a tag input reads its tag from. It is the first
// half of the input's argument, `<target>.<tag key>`.
type MeshBuildingBlockTagInputTarget string

var (
	MeshBuildingBlockTagInputTargets             = enum.Enum[MeshBuildingBlockTagInputTarget]{}
	MeshBuildingBlockTagInputTargetWorkspace     = MeshBuildingBlockTagInputTargets.Entry("WORKSPACE")
	MeshBuildingBlockTagInputTargetProject       = MeshBuildingBlockTagInputTargets.Entry("PROJECT")
	MeshBuildingBlockTagInputTargetPaymentMethod = MeshBuildingBlockTagInputTargets.Entry("PAYMENT_METHOD")
	MeshBuildingBlockTagInputTargetLandingZone   = MeshBuildingBlockTagInputTargets.Entry("LANDING_ZONE")
)

// TagInputTargetSeparator splits the target from the tag key. Only the first one separates them,
// because a tag key may contain a dot itself.
const TagInputTargetSeparator = "."

// TagInputTargetsFor follows what a building block sees: a workspace building block runs in the
// context of its workspace only, while a tenant building block also sees its project, and that
// project's payment method and landing zone.
func TagInputTargetsFor(targetType MeshBuildingBlockType) enum.Enum[MeshBuildingBlockTagInputTarget] {
	if targetType == MeshBuildingBlockTypeWorkspaceLevel.Unwrap() {
		return enum.Of(MeshBuildingBlockTagInputTargetWorkspace)
	}
	return MeshBuildingBlockTagInputTargets
}

type MeshBuildingBlockDefinitionOutputAssignmentType string

var (
	MeshBuildingBlockDefinitionOutputAssignmentTypes                = enum.Enum[MeshBuildingBlockDefinitionOutputAssignmentType]{}
	MeshBuildingBlockDefinitionOutputAssignmentTypeNone             = MeshBuildingBlockDefinitionOutputAssignmentTypes.Entry("NONE")
	MeshBuildingBlockDefinitionOutputAssignmentTypePlatformTenantID = MeshBuildingBlockDefinitionOutputAssignmentTypes.Entry("PLATFORM_TENANT_ID")
	MeshBuildingBlockDefinitionOutputAssignmentTypeSignInURL        = MeshBuildingBlockDefinitionOutputAssignmentTypes.Entry("SIGN_IN_URL")
	MeshBuildingBlockDefinitionOutputAssignmentTypeResourceURL      = MeshBuildingBlockDefinitionOutputAssignmentTypes.Entry("RESOURCE_URL")
	MeshBuildingBlockDefinitionOutputAssignmentTypeSummary          = MeshBuildingBlockDefinitionOutputAssignmentTypes.Entry("SUMMARY")
)

type MeshBuildingBlockDefinitionInput struct {
	DisplayName    string                               `json:"displayName" tfsdk:"display_name"`
	Type           MeshBuildingBlockIOType              `json:"type" tfsdk:"type"`
	AssignmentType MeshBuildingBlockInputAssignmentType `json:"assignmentType" tfsdk:"assignment_type"`
	IsEnvironment  bool                                 `json:"isEnvironment" tfsdk:"is_environment"`
	IsSensitive    bool                                 `json:"isSensitive" tfsdk:"-"`
	// Argument and DefaultValue hold a [types.Secret] when IsSensitive is true and a [types.Any]
	// otherwise. [types.Variant] decodes into X first, so UnmarshalJSON moves a non-sensitive value
	// that also decodes as a Secret over to Y.
	Argument                    types.SecretOrAny `json:"argument" tfsdk:"argument"`
	DefaultValue                types.SecretOrAny `json:"defaultValue" tfsdk:"default_value"`
	UpdateableByConsumer        bool              `json:"updateableByConsumer" tfsdk:"updateable_by_consumer"`
	IsOptional                  bool              `json:"isOptional,omitzero" tfsdk:"is_optional"`
	SelectableValues            types.Set[string] `json:"selectableValues,omitempty" tfsdk:"selectable_values"`
	Description                 *string           `json:"description,omitzero" tfsdk:"description"`
	ValueValidationRegex        *string           `json:"valueValidationRegex,omitzero" tfsdk:"value_validation_regex"`
	ValidationRegexErrorMessage *string           `json:"validationRegexErrorMessage,omitzero" tfsdk:"validation_regex_error_message"`
	// JsonSchema describes the form of an input of type MeshBuildingBlockIOTypeJson, and is set only
	// for that type.
	JsonSchema *string `json:"jsonSchema,omitzero" tfsdk:"json_schema"`
	Condition  *string `json:"condition,omitzero" tfsdk:"condition"`
	// No omitempty: a 0 (the schema default, and what an unknown plan value collapses to) must be sent so
	// the backend stores it verbatim. With omitempty the 0 would be dropped and the backend would assign
	// a position itself, making the applied value differ from the plan.
	DisplayOrder int64 `json:"displayOrder" tfsdk:"display_order"`
}

func (m *MeshBuildingBlockDefinitionInput) UnmarshalJSON(bytes []byte) error {
	type wrapped MeshBuildingBlockDefinitionInput
	var target wrapped
	if err := json.Unmarshal(bytes, &target); err != nil {
		return err
	}
	*m = MeshBuildingBlockDefinitionInput(target)
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
		moveXtoYIfPresent(&m.Argument)
		moveXtoYIfPresent(&m.DefaultValue)
		return errors.Join(errs...)
	case m.Argument.HasY(), m.DefaultValue.HasY():
		return errors.New("got sensitive argument or default_value but variant Y is set instead")
	default:
		return nil
	}
}

type MeshBuildingBlockDefinitionOutput struct {
	DisplayName    string                                          `json:"displayName" tfsdk:"display_name"`
	Type           MeshBuildingBlockIOType                         `json:"type" tfsdk:"type"`
	AssignmentType MeshBuildingBlockDefinitionOutputAssignmentType `json:"assignmentType" tfsdk:"assignment_type"`
	// No omitempty so a 0 is sent, not dropped (see MeshBuildingBlockDefinitionInput.DisplayOrder).
	DisplayOrder int64 `json:"displayOrder" tfsdk:"display_order"`
}

type MeshBuildingBlockDefinitionVersionMetadata struct {
	Uuid             string `json:"uuid"`
	OwnedByWorkspace string `json:"ownedByWorkspace"`
	CreatedOn        string `json:"createdOn"`
}

type MeshBuildingBlockDefinitionVersionSpec struct {
	BuildingBlockDefinitionRef *UuidRef                                     `json:"buildingBlockDefinitionRef" tfsdk:"-"`
	OnlyApplyOncePerTenant     bool                                         `json:"onlyApplyOncePerTenant" tfsdk:"only_apply_once_per_tenant"`
	DeletionMode               BuildingBlockDeletionMode                    `json:"deletionMode" tfsdk:"deletion_mode"`
	Permissions                types.Set[ApiPermission]                     `json:"permissions,omitempty" tfsdk:"permissions"`
	Outputs                    map[string]MeshBuildingBlockDefinitionOutput `json:"outputs" tfsdk:"outputs"`
	VersionNumber              *int64                                       `json:"versionNumber,omitzero" tfsdk:"version_number"`
	State                      *MeshBuildingBlockDefinitionVersionState     `json:"state,omitzero" tfsdk:"state"`
	RunnerRef                  *UuidRef                                     `json:"runnerRef" tfsdk:"runner_ref"`
	// Replaces the deprecated bare-UUID dependencyDefinitionUuids; requires a backend serving it.
	DependencyDefinitionRefs types.Set[UuidRef]                           `json:"dependencyDefinitionRefs,omitempty" tfsdk:"dependency_refs"`
	Implementation           MeshBuildingBlockDefinitionImplementation    `json:"implementation" tfsdk:"implementation"`
	Inputs                   map[string]*MeshBuildingBlockDefinitionInput `json:"inputs" tfsdk:"inputs"`
}

type MeshBuildingBlockDefinitionVersionStatus struct {
	State      MeshBuildingBlockDefinitionVersionState `json:"state" tfsdk:"state"`
	UsageCount *int64                                  `json:"usageCount,omitzero" tfsdk:"usage_count"`
}

type MeshBuildingBlockDefinitionVersion struct {
	Metadata MeshBuildingBlockDefinitionVersionMetadata `json:"metadata" tfsdk:"metadata"`
	Spec     MeshBuildingBlockDefinitionVersionSpec     `json:"spec" tfsdk:"spec"`
	Status   *MeshBuildingBlockDefinitionVersionStatus  `json:"status,omitzero" tfsdk:"status"`
}

// MeshBuildingBlockDefinitionVersionClient has no Get and no Delete: a caller always reads all
// versions of a definition with List, and the versions are deleted together with their definition.
type MeshBuildingBlockDefinitionVersionClient interface {
	List(ctx context.Context, buildingBlockDefinitionUuid string) ([]MeshBuildingBlockDefinitionVersion, error)
	Create(ctx context.Context, ownedByWorkspace string, versionSpec MeshBuildingBlockDefinitionVersionSpec) (*MeshBuildingBlockDefinitionVersion, error)
	Update(ctx context.Context, uuid, ownedByWorkspace string, versionSpec MeshBuildingBlockDefinitionVersionSpec) (*MeshBuildingBlockDefinitionVersion, error)
}

type meshBuildingBlockDefinitionVersionClient struct {
	meshObject internal.MeshObjectClient[MeshBuildingBlockDefinitionVersion]
}

func newBuildingBlockDefinitionVersionClient(ctx context.Context, httpClient internal.HttpClient) meshBuildingBlockDefinitionVersionClient {
	return meshBuildingBlockDefinitionVersionClient{
		meshObject: internal.NewMeshObjectClient[MeshBuildingBlockDefinitionVersion](ctx, httpClient, "v1-preview"),
	}
}

type MeshBuildingBlockDefinitionVersionListFilter struct {
	BuildingBlockDefinitionUuid uuid.UUID `json:"buildingBlockDefinitionUuid,omitzero"`
}

func (c meshBuildingBlockDefinitionVersionClient) List(ctx context.Context, buildingBlockDefinitionUuid string) ([]MeshBuildingBlockDefinitionVersion, error) {
	definitionUuid, err := uuid.Parse(buildingBlockDefinitionUuid)
	if err != nil {
		return nil, fmt.Errorf("building block definition %q: %w", buildingBlockDefinitionUuid, err)
	}
	return c.meshObject.List(ctx, http.WithUrlQuery(MeshBuildingBlockDefinitionVersionListFilter{
		BuildingBlockDefinitionUuid: definitionUuid,
	}))
}

func (c meshBuildingBlockDefinitionVersionClient) Create(ctx context.Context, ownedByWorkspace string, versionSpec MeshBuildingBlockDefinitionVersionSpec) (*MeshBuildingBlockDefinitionVersion, error) {
	return c.meshObject.Post(ctx, MeshBuildingBlockDefinitionVersion{
		Metadata: MeshBuildingBlockDefinitionVersionMetadata{
			OwnedByWorkspace: ownedByWorkspace,
		},
		Spec: versionSpec,
	})
}

func (c meshBuildingBlockDefinitionVersionClient) Update(ctx context.Context, uuid, ownedByWorkspace string, versionSpec MeshBuildingBlockDefinitionVersionSpec) (*MeshBuildingBlockDefinitionVersion, error) {
	return c.meshObject.Put(ctx, uuid, MeshBuildingBlockDefinitionVersion{
		Metadata: MeshBuildingBlockDefinitionVersionMetadata{
			Uuid:             uuid,
			OwnedByWorkspace: ownedByWorkspace,
		},
		Spec: versionSpec,
	})
}
