package client

type MeshLandingZone struct {
	Metadata MeshLandingZoneMetadata `json:"metadata" tfsdk:"metadata"`
	Spec     MeshLandingZoneSpec     `json:"spec" tfsdk:"spec"`
	Status   MeshLandingZoneStatus   `json:"status,omitzero" tfsdk:"status"`
}

type MeshLandingZoneMetadata struct {
	Name             string              `json:"name" tfsdk:"name"`
	OwnedByWorkspace string              `json:"ownedByWorkspace" tfsdk:"owned_by_workspace"`
	Tags             map[string][]string `json:"tags" tfsdk:"tags"`
}

type MeshLandingZoneSpec struct {
	DisplayName                 string `json:"displayName" tfsdk:"display_name"`
	Description                 string `json:"description" tfsdk:"description"`
	AutomateDeletionApproval    bool   `json:"automateDeletionApproval" tfsdk:"automate_deletion_approval"`
	AutomateDeletionReplication bool   `json:"automateDeletionReplication" tfsdk:"automate_deletion_replication"`
	// Nullable in the API, where absent means "keep the stored value" — hence no `,omitempty`: the
	// schema defaults this to false, so the provider always states the value it wants and never
	// asks the backend to keep whatever is stored.
	Restricted                   bool                               `json:"restricted" tfsdk:"restricted"`
	InfoLink                     *string                            `json:"infoLink,omitzero" tfsdk:"info_link"`
	PlatformRef                  UuidRef                            `json:"platformRef" tfsdk:"platform_ref"`
	PlatformProperties           *MeshLandingZonePlatformProperties `json:"platformProperties,omitzero" tfsdk:"platform_properties"`
	Quotas                       []MeshLandingZoneQuota             `json:"quotas" tfsdk:"quotas"`
	MandatoryBuildingBlockRefs   []UuidRef                          `json:"mandatoryBuildingBlockRefs" tfsdk:"mandatory_building_block_refs"`
	RecommendedBuildingBlockRefs []UuidRef                          `json:"recommendedBuildingBlockRefs" tfsdk:"recommended_building_block_refs"`
}

type MeshLandingZoneStatus struct {
	Disabled   bool `json:"disabled" tfsdk:"disabled"`
	Restricted bool `json:"restricted" tfsdk:"restricted"`
}

type MeshLandingZonePlatformProperties struct {
	Type       string                        `json:"type" tfsdk:"type"`
	Aws        *AwsPlatformProperties        `json:"aws" tfsdk:"aws"`
	Aks        *AksPlatformProperties        `json:"aks" tfsdk:"aks"`
	Azure      *AzurePlatformProperties      `json:"azure" tfsdk:"azure"`
	AzureRg    *AzureRgPlatformProperties    `json:"azurerg" tfsdk:"azurerg"`
	Custom     *CustomPlatformProperties     `json:"custom" tfsdk:"custom"`
	Gcp        *GcpPlatformProperties        `json:"gcp" tfsdk:"gcp"`
	Kubernetes *KubernetesPlatformProperties `json:"kubernetes" tfsdk:"kubernetes"`
	OpenShift  *OpenShiftPlatformProperties  `json:"openshift" tfsdk:"openshift"`
}

type MeshLandingZoneQuota struct {
	Key   string `json:"key" tfsdk:"key"`
	Value int64  `json:"value" tfsdk:"value"`
}

type MeshLandingZoneFilter struct {
	PlatformUuid     *string `json:"platformUuid"`
	Identifier       *string `json:"identifier"`
	DisplayName      *string `json:"displayName"`
	Restricted       *bool   `json:"restricted"`
	OwnedByWorkspace *string `json:"ownedByWorkspace"`
}

type MeshLandingZoneClient = MeshObjectClient[MeshLandingZone, MeshLandingZoneFilter]

func (lz MeshLandingZone) meshObjectId() string {
	return lz.Metadata.Name
}
