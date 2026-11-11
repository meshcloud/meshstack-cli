package client

type MeshProject struct {
	Metadata MeshProjectMetadata `json:"metadata" tfsdk:"metadata"`
	Spec     MeshProjectSpec     `json:"spec" tfsdk:"spec"`
}

type MeshProjectMetadata struct {
	Name             string  `json:"name" tfsdk:"name"`
	OwnedByWorkspace string  `json:"ownedByWorkspace" tfsdk:"owned_by_workspace"`
	CreatedOn        string  `json:"createdOn,omitzero" tfsdk:"created_on"`
	DeletedOn        *string `json:"deletedOn,omitzero" tfsdk:"deleted_on"`
}

type MeshProjectSpec struct {
	DisplayName                       string              `json:"displayName" tfsdk:"display_name"`
	Tags                              map[string][]string `json:"tags" tfsdk:"tags"`
	PaymentMethodIdentifier           *string             `json:"paymentMethodIdentifier" tfsdk:"payment_method_identifier"`
	SubstitutePaymentMethodIdentifier *string             `json:"substitutePaymentMethodIdentifier" tfsdk:"substitute_payment_method_identifier"`
}

func NewMeshProjectId(workspace, name string) string {
	return workspace + "." + name
}

type MeshProjectFilter struct {
	WorkspaceIdentifier string  `json:"workspaceIdentifier"`
	PaymentIdentifier   *string `json:"paymentIdentifier"`
}

type MeshProjectClient = MeshObjectClient[MeshProject, MeshProjectFilter]

func (project MeshProject) meshObjectId() string {
	return NewMeshProjectId(project.Metadata.OwnedByWorkspace, project.Metadata.Name)
}
