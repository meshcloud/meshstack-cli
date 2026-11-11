package client

type MeshPaymentMethod struct {
	Metadata MeshPaymentMethodMetadata `json:"metadata" tfsdk:"metadata"`
	Spec     MeshPaymentMethodSpec     `json:"spec" tfsdk:"spec"`
}

type MeshPaymentMethodMetadata struct {
	Name             string  `json:"name" tfsdk:"name"`
	OwnedByWorkspace string  `json:"ownedByWorkspace" tfsdk:"owned_by_workspace"`
	CreatedOn        string  `json:"createdOn,omitzero" tfsdk:"created_on"`
	DeletedOn        *string `json:"deletedOn,omitzero" tfsdk:"deleted_on"`
}

type MeshPaymentMethodSpec struct {
	DisplayName    string              `json:"displayName" tfsdk:"display_name"`
	ExpirationDate *string             `json:"expirationDate,omitzero" tfsdk:"expiration_date"`
	Amount         *int64              `json:"amount,omitzero" tfsdk:"amount"`
	Tags           map[string][]string `json:"tags,omitempty" tfsdk:"tags"`
}

type MeshPaymentMethodClient = MeshObjectClient[MeshPaymentMethod, NoFilter]

func (paymentMethod MeshPaymentMethod) meshObjectId() string {
	return paymentMethod.Metadata.Name
}
