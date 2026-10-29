package client

import (
	"context"
	"encoding/json/jsontext"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/url"
	"strings"
	"time"
	"uuid"

	"github.com/meshcloud/meshstack-cli/client/http"
	"github.com/meshcloud/meshstack-cli/client/internal"
	"github.com/meshcloud/meshstack-cli/client/types/enum"
	"github.com/meshcloud/meshstack-cli/client/types/variant"
	"github.com/meshcloud/meshstack-cli/client/types/xurl"
	"github.com/meshcloud/meshstack-cli/internal/json"
)

type MeshRunnerRun struct {
	ApiVersion string                `json:"apiVersion"`
	Kind       string                `json:"kind"`
	Metadata   MeshRunnerRunMetadata `json:"metadata"`
	Spec       MeshRunnerRunSpec     `json:"spec"`
	Status     string                `json:"status"`
	Links      MeshRunnerRunLinks    `json:"_links"`
}

type MeshRunnerRunMetadata struct {
	Uuid uuid.UUID `json:"uuid"`
}

type MeshRunnerRunSpec struct {
	RunNumber     int                        `json:"runNumber"`
	Behavior      string                     `json:"behavior"`
	BuildingBlock MeshRunnerRunBuildingBlock `json:"buildingBlock"`
	Definition    MeshRunnerRunDefinition    `json:"buildingBlockDefinition"`
	RunToken      string                     `json:"runToken"`
	Trigger       *MeshRunnerRunTrigger      `json:"trigger,omitzero"`
}

type MeshRunnerRunBuildingBlock struct {
	Uuid string                         `json:"uuid"`
	Spec MeshRunnerRunBuildingBlockSpec `json:"spec"`
}

type MeshRunnerRunBuildingBlockSpec struct {
	DisplayName            string                     `json:"displayName"`
	WorkspaceIdentifier    string                     `json:"workspaceIdentifier"`
	ProjectIdentifier      string                     `json:"projectIdentifier,omitempty"`
	FullPlatformIdentifier string                     `json:"fullPlatformIdentifier,omitempty"`
	Inputs                 []MeshRunnerRunInput       `json:"inputs"`
	ParentBuildingBlocks   []MeshRunnerRunParentBlock `json:"parentBuildingBlocks"`
}

type MeshRunnerRunParentBlock struct {
	BuildingBlockUuid string `json:"buildingBlockUuid"`
	DefinitionUuid    string `json:"definitionUuid"`
}

type MeshRunnerRunInput struct {
	Key string `json:"key"`
	// Value holds a *big.Int for an INTEGER, so that it keeps every digit.
	Value       any                                 `json:"value"`
	Type        enum.Entry[MeshBuildingBlockIOType] `json:"type"`
	IsSensitive bool                                `json:"isSensitive"`
	Env         bool                                `json:"isEnvironment"`
}

func (i *MeshRunnerRunInput) UnmarshalJSON(data []byte) error {
	type wire MeshRunnerRunInput
	var target struct {
		wire

		Value jsontext.Value `json:"value"`
	}
	if err := json.Unmarshal(data, &target); err != nil {
		return err
	}
	*i = MeshRunnerRunInput(target.wire)
	// An encrypted INTEGER is a string until it is decrypted.
	if i.Type == MeshBuildingBlockIOTypeInteger && target.Value.Kind() == '0' {
		i.Value = new(big.Int)
	}
	if err := json.Unmarshal(target.Value, &i.Value); err != nil {
		return fmt.Errorf("input %s: %w", i.Key, err)
	}
	return nil
}

type MeshRunnerRunDefinition struct {
	Uuid string                      `json:"uuid"`
	Spec MeshRunnerRunDefinitionSpec `json:"spec"`
}

type MeshRunnerRunDefinitionSpec struct {
	WorkspaceIdentifier string                      `json:"workspaceIdentifier"`
	Version             int                         `json:"version"`
	Implementation      MeshRunnerRunImplementation `json:"implementation"`
}

type MeshRunnerRunTrigger struct {
	Reason string              `json:"reason"`
	Author MeshRunnerRunAuthor `json:"author"`
}

type MeshRunnerRunAuthor struct {
	Identifier  string  `json:"identifier"`
	Type        string  `json:"type"`
	DisplayName *string `json:"displayName"`
	Username    *string `json:"username"`
	Email       *string `json:"email"`
	Euid        *string `json:"euid"`
}

type MeshRunnerRunLinks struct {
	Self             MeshRunnerRunLink `json:"self"`
	RegisterSource   MeshRunnerRunLink `json:"registerSource"`
	UpdateSource     MeshRunnerRunLink `json:"updateSource"`
	MeshstackBaseUrl MeshRunnerRunLink `json:"meshstackBaseUrl"`
	// Artifact is set only when an APPLY run applies the plan of its DETECT run.
	Artifact       MeshRunnerRunLink `json:"artifact,omitzero"`
	ArtifactUpload MeshRunnerRunLink `json:"artifactUpload,omitzero"`
	// TfStateLock is empty on a meshStack that cannot lock the state.
	TfStateLock MeshRunnerRunLink `json:"tfStateLock,omitzero"`
}

type MeshRunnerRunLink struct {
	Href      string `json:"href"`
	Templated bool   `json:"templated,omitzero"`
}

func (l MeshRunnerRunLink) parse(name string) (*url.URL, error) {
	if l.Href == "" {
		return nil, fmt.Errorf("the run has no %s link", name)
	}
	return url.Parse(l.Href)
}

type MeshRunnerImplementationType string

var (
	MeshRunnerImplementationTypes              = enum.Enum[MeshRunnerImplementationType]{}
	MeshRunnerImplementationTypeManual         = MeshRunnerImplementationTypes.Entry("MANUAL")
	MeshRunnerImplementationTypeTerraform      = MeshRunnerImplementationTypes.Entry("TERRAFORM")
	MeshRunnerImplementationTypeGithubWorkflow = MeshRunnerImplementationTypes.Entry("GITHUB_WORKFLOW")
	MeshRunnerImplementationTypeGitlabCiCd     = MeshRunnerImplementationTypes.Entry("GITLAB_CICD")
	MeshRunnerImplementationTypeAzureDevops    = MeshRunnerImplementationTypes.Entry("AZURE_DEVOPS")
)

// MeshRunnerRunImplementation sets no variant for a type this client does not know.
type MeshRunnerRunImplementation struct {
	Type           enum.Entry[MeshRunnerImplementationType]
	Manual         *MeshRunnerManualImplementation
	Terraform      *MeshRunnerTerraformImplementation
	GithubWorkflow *MeshRunnerGithubImplementation
	GitlabCiCd     *MeshRunnerGitlabImplementation
	AzureDevops    *MeshRunnerAzureDevopsImplementation
}

type MeshRunnerManualImplementation struct{}

type MeshRunnerTerraformImplementation struct {
	TerraformVersion           string               `json:"terraformVersion"`
	RepositoryUrl              string               `json:"repositoryUrl"`
	RepositoryPath             *string              `json:"repositoryPath,omitzero"`
	RefName                    *string              `json:"refName,omitzero"`
	SshPrivateKey              *string              `json:"sshPrivateKey,omitzero"`
	KnownHost                  *MeshRunnerKnownHost `json:"knownHost,omitzero"`
	UseMeshHttpBackendFallback bool                 `json:"useMeshHttpBackendFallback"`
	PreRunScript               *string              `json:"preRunScript,omitzero"`
	Async                      bool                 `json:"async"`
}

type MeshRunnerKnownHost struct {
	Host     string `json:"host"`
	KeyType  string `json:"keyType"`
	KeyValue string `json:"keyValue"`
}

type MeshRunnerGithubImplementation struct {
	GithubBaseUrl      string  `json:"githubBaseUrl"`
	Owner              string  `json:"owner"`
	AppId              string  `json:"appId"`
	AppPem             string  `json:"appPem"`
	Repository         string  `json:"repository"`
	Branch             string  `json:"branch"`
	ApplyWorkflow      string  `json:"applyWorkflow"`
	DestroyWorkflow    *string `json:"destroyWorkflow,omitzero"`
	Async              bool    `json:"async"`
	OmitRunObjectInput bool    `json:"omitRunObjectInput"`
}

type MeshRunnerGitlabImplementation struct {
	GitlabBaseUrl        string `json:"gitlabBaseUrl"`
	ProjectId            string `json:"projectId"`
	RefName              string `json:"refName"`
	PipelineTriggerToken string `json:"pipelineTriggerToken"`
}

type MeshRunnerAzureDevopsImplementation struct {
	AzureDevOpsBaseUrl  string  `json:"azureDevOpsBaseUrl"`
	Organization        string  `json:"organization"`
	Project             string  `json:"project"`
	PipelineId          string  `json:"pipelineId"`
	PersonalAccessToken string  `json:"personalAccessToken"`
	Async               bool    `json:"async"`
	RefName             *string `json:"refName,omitzero"`
}

func (m MeshRunnerRunImplementation) InferType() (enum.Entry[MeshRunnerImplementationType], error) {
	result, err := variant.InferType(
		variant.NewCandidate(MeshRunnerImplementationTypeManual, m.Manual != nil),
		variant.NewCandidate(MeshRunnerImplementationTypeTerraform, m.Terraform != nil),
		variant.NewCandidate(MeshRunnerImplementationTypeGithubWorkflow, m.GithubWorkflow != nil),
		variant.NewCandidate(MeshRunnerImplementationTypeGitlabCiCd, m.GitlabCiCd != nil),
		variant.NewCandidate(MeshRunnerImplementationTypeAzureDevops, m.AzureDevops != nil),
	)
	if err != nil {
		return "", fmt.Errorf("cannot infer implementation type: %w", err)
	}
	return result, nil
}

func (m MeshRunnerRunImplementation) MarshalJSON() ([]byte, error) {
	implementationType := m.Type
	if len(implementationType) == 0 {
		var err error
		if implementationType, err = m.InferType(); err != nil {
			return nil, err
		}
	}
	switch {
	case m.Terraform != nil:
		return marshalFlat(implementationType, m.Terraform)
	case m.GithubWorkflow != nil:
		return marshalFlat(implementationType, m.GithubWorkflow)
	case m.GitlabCiCd != nil:
		return marshalFlat(implementationType, m.GitlabCiCd)
	case m.AzureDevops != nil:
		return marshalFlat(implementationType, m.AzureDevops)
	default:
		return marshalFlat(implementationType, &MeshRunnerManualImplementation{})
	}
}

func marshalFlat[V any](implementationType enum.Entry[MeshRunnerImplementationType], implementation *V) ([]byte, error) {
	var fields map[string]jsontext.Value
	if encoded, err := json.Marshal(implementation); err != nil {
		return nil, err
	} else if err := json.Unmarshal(encoded, &fields); err != nil {
		return nil, err
	}
	var err error
	if fields["type"], err = json.Marshal(implementationType); err != nil {
		return nil, err
	}
	return json.Marshal(fields)
}

func (m *MeshRunnerRunImplementation) UnmarshalJSON(data []byte) error {
	var typed struct {
		Type enum.Entry[MeshRunnerImplementationType] `json:"type"`
	}
	if err := json.Unmarshal(data, &typed); err != nil {
		return err
	}
	*m = MeshRunnerRunImplementation{Type: typed.Type}
	var err error
	switch typed.Type {
	case MeshRunnerImplementationTypeManual:
		m.Manual = &MeshRunnerManualImplementation{}
	case MeshRunnerImplementationTypeTerraform:
		m.Terraform, err = unmarshalFlat[MeshRunnerTerraformImplementation](data)
	case MeshRunnerImplementationTypeGithubWorkflow:
		m.GithubWorkflow, err = unmarshalFlat[MeshRunnerGithubImplementation](data)
	case MeshRunnerImplementationTypeGitlabCiCd:
		m.GitlabCiCd, err = unmarshalFlat[MeshRunnerGitlabImplementation](data)
	case MeshRunnerImplementationTypeAzureDevops:
		m.AzureDevops, err = unmarshalFlat[MeshRunnerAzureDevopsImplementation](data)
	}
	return err
}

func unmarshalFlat[V any](data []byte) (*V, error) {
	implementation := new(V)
	if err := json.Unmarshal(data, implementation); err != nil {
		return nil, err
	}
	return implementation, nil
}

type MeshRunnerRunSourceRegistration struct {
	Source MeshRunnerRunSource             `json:"source"`
	Steps  []MeshRunnerRunStepRegistration `json:"steps"`
}

type MeshRunnerRunSource struct {
	Id          uuid.UUID `json:"id"`
	ExternalId  *string   `json:"externalId,omitzero"`
	ExternalUrl *string   `json:"externalUrl,omitzero"`
}

type MeshRunnerRunStepRegistration struct {
	Id          string  `json:"id"`
	DisplayName string  `json:"displayName"`
	Status      *string `json:"status"`
}

type MeshRunnerRunStatusUpdate struct {
	BlockRunId      uuid.UUID                                `json:"blockRunId,omitzero"`
	Source          uuid.UUID                                `json:"source,omitzero"`
	Type            enum.Entry[MeshRunnerImplementationType] `json:"type,omitempty"`
	Status          *string                                  `json:"status"`
	CreatedOn       time.Time                                `json:"createdOn,omitzero"`
	Summary         *string                                  `json:"summary"`
	Steps           []MeshRunnerRunStepStatusUpdate          `json:"steps"`
	ChangesDetected *bool                                    `json:"changesDetected,omitzero"`
}

type MeshRunnerRunStepStatusUpdate struct {
	Id            string                         `json:"id"`
	DisplayName   string                         `json:"displayName"`
	Status        *string                        `json:"status,omitzero"`
	UserMessage   *string                        `json:"userMessage,omitzero"`
	SystemMessage *string                        `json:"systemMessage,omitzero"`
	Outputs       map[string]MeshRunnerRunOutput `json:"outputs,omitempty"`
}

type MeshRunnerRunOutput struct {
	Value     any    `json:"value"`
	Type      string `json:"type"`
	Sensitive bool   `json:"isSensitive"`
}

type MeshRunnerRunStatusUpdateResult struct {
	Abort bool `json:"runAborted"`
}

const (
	runMediaType = "application/vnd.meshcloud.api.meshbuildingblockrun.v1.hal+json"

	maxArtifactBytes = 128 << 20
)

var ErrNoRunToClaim = errors.New("no run to claim")

type MeshRunnerClient interface {
	ClaimRun(ctx context.Context, runnerUuid uuid.UUID) (MeshRunnerRun, error)
	// ForRun authorizes with the run's token, which every request about the run needs.
	ForRun(run MeshRunnerRun) MeshRunnerRunClient
	// ForRunUuid authorizes with the runner's own credentials and can only register a source and patch its status.
	ForRunUuid(runUuid uuid.UUID) MeshRunnerRunClient
}

type MeshRunnerRunClient interface {
	RegisterSource(ctx context.Context, registration MeshRunnerRunSourceRegistration) error
	PatchStatus(ctx context.Context, sourceId uuid.UUID, update MeshRunnerRunStatusUpdate) (MeshRunnerRunStatusUpdateResult, error)
	DownloadArtifact(ctx context.Context, w io.Writer) error
	UploadArtifact(ctx context.Context, path string) error
}

// NewRunnerClient builds only the runner API, without the meshObject clients of New.
func NewRunnerClient(endpoint xurl.URL, client http.Client, auth http.Authorization) MeshRunnerClient {
	return newRunnerClient(internal.HttpClient{AuthorizedClient: client.WithAuthorization(auth), EndpointUrl: endpoint})
}

func newRunnerClient(httpClient internal.HttpClient) MeshRunnerClient {
	return meshRunnerClient{runnerHttpClient{httpClient.AuthorizedClient}, httpClient.EndpointUrl}
}

type meshRunnerClient struct {
	http     runnerHttpClient
	endpoint xurl.URL
}

func (c meshRunnerClient) ClaimRun(ctx context.Context, runnerUuid uuid.UUID) (MeshRunnerRun, error) {
	claimUrl := c.endpoint.JoinPath("api", "meshobjects", "meshbuildingblockruns", "create")
	claimUrl.RawQuery = url.Values{"forRunnerUuid": {runnerUuid.String()}}.Encode()
	run, err := c.http.do[MeshRunnerRun](ctx, http.MethodPost, claimUrl, http.WithHeader("Accept", runMediaType), http.WithHeader("Content-Type", runMediaType))
	if httpErr, ok := errors.AsType[http.Error](err); ok && httpErr.IsNotFound() {
		return run, ErrNoRunToClaim
	}
	if err == nil && run.Metadata.Uuid == (uuid.UUID{}) {
		err = errors.New("the claimed run has no uuid")
	}
	return run, err
}

func (c meshRunnerClient) ForRun(run MeshRunnerRun) MeshRunnerRunClient {
	runToken := http.BearerToken(run.Spec.RunToken)
	return meshRunnerRunClient{runnerHttpClient{c.http.WithAuthorization(runToken)}, run.Links}
}

func (c meshRunnerClient) ForRunUuid(runUuid uuid.UUID) MeshRunnerRunClient {
	registerSource := c.endpoint.JoinPath("api", "meshobjects", "meshbuildingblockruns", runUuid.String(), "status", "source").String()
	return meshRunnerRunClient{c.http, MeshRunnerRunLinks{
		RegisterSource: MeshRunnerRunLink{Href: registerSource},
		UpdateSource:   MeshRunnerRunLink{Href: registerSource + "/{sourceId}", Templated: true},
	}}
}

type meshRunnerRunClient struct {
	http  runnerHttpClient
	links MeshRunnerRunLinks
}

func (c meshRunnerRunClient) RegisterSource(ctx context.Context, registration MeshRunnerRunSourceRegistration) error {
	registerUrl, err := c.links.RegisterSource.parse("registerSource")
	if err != nil {
		return err
	}
	_, err = c.http.do[[]byte](ctx, http.MethodPost, registerUrl, http.WithJsonPayload(registration, runMediaType), http.Retryable())
	if httpErr, ok := errors.AsType[http.Error](err); ok && httpErr.IsConflict() {
		return nil
	}
	return err
}

func (c meshRunnerRunClient) PatchStatus(ctx context.Context, sourceId uuid.UUID, update MeshRunnerRunStatusUpdate) (MeshRunnerRunStatusUpdateResult, error) {
	updateSource := MeshRunnerRunLink{Href: strings.ReplaceAll(c.links.UpdateSource.Href, "{sourceId}", sourceId.String())}
	updateUrl, err := updateSource.parse("updateSource")
	if err != nil {
		return MeshRunnerRunStatusUpdateResult{}, err
	}
	return c.http.do[MeshRunnerRunStatusUpdateResult](ctx, http.MethodPatch, updateUrl, http.WithJsonPayload(update, runMediaType), http.Retryable())
}

func (c meshRunnerRunClient) DownloadArtifact(ctx context.Context, w io.Writer) error {
	downloadUrl, err := c.links.Artifact.parse("artifact")
	if err != nil {
		return err
	}
	capped := &cappedWriter{w, maxArtifactBytes}
	return c.http.DoStreamingRequest(ctx, http.MethodGet, downloadUrl, capped, c.http.version(), http.WithHeader("Accept", "application/octet-stream"))
}

func (c meshRunnerRunClient) UploadArtifact(ctx context.Context, path string) error {
	uploadUrl, err := c.links.ArtifactUpload.parse("artifactUpload")
	if err != nil {
		return err
	}
	_, err = c.http.do[[]byte](ctx, http.MethodPut, uploadUrl, http.WithBodyFile("application/octet-stream", path), http.WithHeader("Accept", "*/*"))
	return err
}

type runnerHttpClient struct {
	http.AuthorizedClient
}

func (c runnerHttpClient) do[R any](ctx context.Context, method string, url *url.URL, options ...http.RequestOption) (R, error) {
	return c.DoRequest[R](ctx, method, url, append(options, c.version())...)
}

func (c runnerHttpClient) version() http.RequestOption {
	return http.WithHeader("X-Meshcloud-Runner-Version", c.UserAgent.Version)
}

// cappedWriter keeps a misbehaving server from filling the disk.
type cappedWriter struct {
	io.Writer

	remaining int64
}

func (w *cappedWriter) Write(p []byte) (int, error) {
	if int64(len(p)) > w.remaining {
		return 0, fmt.Errorf("the artifact exceeds %d bytes", maxArtifactBytes)
	}
	w.remaining -= int64(len(p))
	return w.Writer.Write(p)
}
