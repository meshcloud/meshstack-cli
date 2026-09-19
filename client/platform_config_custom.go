package client

type CustomPlatformConfig struct {
	PlatformTypeRef NamedRef              `json:"platformTypeRef" tfsdk:"platform_type_ref"`
	Metering        *CustomMeteringConfig `json:"metering,omitzero" tfsdk:"metering"`
}

type CustomMeteringConfig struct {
	Processing *MeshPlatformMeteringProcessingConfig `json:"processing,omitzero" tfsdk:"processing"`
}
