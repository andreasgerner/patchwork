package v1

import (
	"encoding/json"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// NestedObject holds free-form nested YAML mirroring a target resource's
// structure. Used for defaults (leaves are strings), deletes (leaves are
// string arrays), and conditions (leaves are strings to match against).
//
// +kubebuilder:validation:Type=object
// +kubebuilder:pruning:PreserveUnknownFields
// +kubebuilder:validation:XPreserveUnknownFields
type NestedObject struct {
	Data map[string]interface{} `json:"-"`
}

func (n NestedObject) MarshalJSON() ([]byte, error) {
	if n.Data == nil {
		return []byte("{}"), nil
	}
	return json.Marshal(n.Data)
}

func (n *NestedObject) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &n.Data)
}

func (n NestedObject) DeepCopy() NestedObject {
	return NestedObject{Data: cloneMap(n.Data)}
}

func cloneMap(m map[string]interface{}) map[string]interface{} {
	if m == nil {
		return nil
	}
	out := make(map[string]interface{}, len(m))
	for k, v := range m {
		switch val := v.(type) {
		case map[string]interface{}:
			out[k] = cloneMap(val)
		case []interface{}:
			cp := make([]interface{}, len(val))
			copy(cp, val)
			out[k] = cp
		default:
			out[k] = v
		}
	}
	return out
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced,shortName=pr
// +kubebuilder:printcolumn:name="Target",type=string,JSONPath=`.spec.target.kind`
// +kubebuilder:printcolumn:name="Overwrite",type=boolean,JSONPath=`.spec.overwrite`
// +kubebuilder:printcolumn:name="Targets",type=integer,JSONPath=`.status.targetCount`
// +kubebuilder:printcolumn:name="Active",type=boolean,JSONPath=`.status.active`
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

// PatchRule defines patches to apply to matching target resources.
type PatchRule struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   PatchRuleSpec   `json:"spec"`
	Status PatchRuleStatus `json:"status,omitempty"`
}

// PatchRuleSpec is the spec for a PatchRule resource.
type PatchRuleSpec struct {
	// Target identifies which resources to patch.
	Target TargetRef `json:"target"`

	// Overwrite existing values. When false (default), only sets missing keys.
	// +optional
	// +kubebuilder:default=false
	Overwrite bool `json:"overwrite,omitempty"`

	// Additions: nested YAML of values to set, mirroring the target structure.
	// +optional
	Additions NestedObject `json:"additions,omitempty"`

	// Removals: nested YAML where leaves are arrays of key names to remove.
	// +optional
	Removals NestedObject `json:"removals,omitempty"`
}

// TargetRef identifies a resource type and optional match conditions.
type TargetRef struct {
	// APIVersion of the target (e.g. "networking.k8s.io/v1").
	APIVersion string `json:"apiVersion"`
	// Kind of the target (e.g. "Ingress").
	Kind string `json:"kind"`
	// Conditions: nested YAML of values that must all match on the target.
	// +optional
	Conditions NestedObject `json:"conditions,omitempty"`
}

// PatchRuleStatus reports the observed state.
type PatchRuleStatus struct {
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
	// +optional
	TargetCount int32 `json:"targetCount,omitempty"`
	// +optional
	Active bool `json:"active,omitempty"`
}

// +kubebuilder:object:root=true

// PatchRuleList is a list of PatchRule resources.
type PatchRuleList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`

	Items []PatchRule `json:"items"`
}
