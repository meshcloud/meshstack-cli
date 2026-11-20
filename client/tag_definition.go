package client

type MeshTagDefinitionMetadata = MeshObjectByNameMetadata

type MeshTagDefinition struct {
	Metadata MeshTagDefinitionMetadata `json:"metadata" tfsdk:"metadata"`
	Spec     MeshTagDefinitionSpec     `json:"spec" tfsdk:"spec"`
}

func (d MeshTagDefinition) meshObjectId() string {
	return d.Metadata.meshObjectId()
}

type MeshTagDefinitionSpec struct {
	TargetKind     string                     `json:"targetKind" tfsdk:"target_kind"`
	Key            string                     `json:"key" tfsdk:"key"`
	ValueType      MeshTagDefinitionValueType `json:"valueType" tfsdk:"value_type"`
	Description    string                     `json:"description" tfsdk:"description"`
	DisplayName    string                     `json:"displayName" tfsdk:"display_name"`
	SortOrder      int64                      `json:"sortOrder" tfsdk:"sort_order"`
	Mandatory      bool                       `json:"mandatory" tfsdk:"mandatory"`
	Immutable      bool                       `json:"immutable" tfsdk:"immutable"`
	Restricted     bool                       `json:"restricted" tfsdk:"restricted"`
	ReplicationKey *string                    `json:"replicationKey,omitzero" tfsdk:"replication_key"`
}

type MeshTagDefinitionValueType struct {
	String       *TagValueString       `json:"string,omitzero" tfsdk:"string"`
	Email        *TagValueEmail        `json:"email,omitzero" tfsdk:"email"`
	Integer      *TagValueInteger      `json:"integer,omitzero" tfsdk:"integer"`
	Number       *TagValueNumber       `json:"number,omitzero" tfsdk:"number"`
	SingleSelect *TagValueSingleSelect `json:"singleSelect,omitzero" tfsdk:"single_select"`
	MultiSelect  *TagValueMultiSelect  `json:"multiSelect,omitzero" tfsdk:"multi_select"`
}

type TagValueString struct {
	DefaultValue    *string `json:"defaultValue,omitzero" tfsdk:"default_value"`
	ValidationRegex *string `json:"validationRegex,omitzero" tfsdk:"validation_regex"`
}

type TagValueEmail struct {
	DefaultValue    *string `json:"defaultValue,omitzero" tfsdk:"default_value"`
	ValidationRegex *string `json:"validationRegex,omitzero" tfsdk:"validation_regex"`
}

type TagValueInteger struct {
	DefaultValue *int64 `json:"defaultValue,omitzero" tfsdk:"default_value"`
}

type TagValueNumber struct {
	DefaultValue *float64 `json:"defaultValue,omitzero" tfsdk:"default_value"`
}

type TagValueSingleSelect struct {
	Options      []string `json:"options,omitempty" tfsdk:"options"`
	DefaultValue *string  `json:"defaultValue,omitzero" tfsdk:"default_value"`
}

type TagValueMultiSelect struct {
	Options      []string  `json:"options,omitempty" tfsdk:"options"`
	DefaultValue *[]string `json:"defaultValue,omitzero" tfsdk:"default_value"`
}

type MeshTagDefinitionClient = MeshObjectClient[MeshTagDefinition, NoFilter]
