package client

// NamedRef is every {name, kind} reference on the wire, and a ref with more fields embeds it. Keep
// it in step with meshRefByName in internal/provider/schema_utils.go of terraform-provider-meshstack.
type NamedRef struct {
	Name string `json:"name" tfsdk:"name"`
	Kind string `json:"kind" tfsdk:"kind"`
}

// UuidRef is every {uuid, kind} reference on the wire, and a ref with more fields embeds it. Keep
// it in step with meshRefByUuid in internal/provider/schema_utils.go of terraform-provider-meshstack.
type UuidRef struct {
	Uuid string `json:"uuid" tfsdk:"uuid"`
	Kind string `json:"kind" tfsdk:"kind"`
}

type SupportedPlatformRef struct {
	Kind string  `json:"kind" tfsdk:"kind"`
	Name *string `json:"name,omitzero" tfsdk:"name"`
	Uuid *string `json:"uuid,omitzero" tfsdk:"uuid"`
}
