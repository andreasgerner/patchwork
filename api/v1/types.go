package v1

import (
	"encoding/json"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// NestedObject holds free-form nested YAML mirroring a target resource's
// structure. Used for additions (leaves are strings), removals (leaves are
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
		return []byte("null"), nil
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
			for i, elem := range val {
				if nested, ok := elem.(map[string]interface{}); ok {
					cp[i] = cloneMap(nested)
				} else {
					cp[i] = elem
				}
			}
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
// +kubebuilder:printcolumn:name="Priority",type=integer,JSONPath=`.spec.priority`
// +kubebuilder:printcolumn:name="Overwrite",type=boolean,JSONPath=`.spec.overwrite`
// +kubebuilder:printcolumn:name="Targets",type=integer,JSONPath=`.status.targetCount`
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`
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

	// Priority determines which PatchRule wins when multiple rules target the same
	// key paths on the same resource. Higher values take precedence. When equal,
	// the rule with the earlier creation timestamp wins.
	// +optional
	// +kubebuilder:default=1
	// +kubebuilder:validation:Minimum=0
	Priority *int32 `json:"priority,omitempty"`

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

// TargetState records what was applied to a single target resource and what the
// original values were before patching. Used for reverting on deletion or spec change.
type TargetState struct {
	// AppliedAdditions: the additions that were actually written to the target.
	// +optional
	AppliedAdditions NestedObject `json:"appliedAdditions,omitempty"`

	// PriorValues: original values on the target that were overwritten by additions.
	// Keys that did not exist before patching are absent (revert = delete them).
	// +optional
	PriorValues NestedObject `json:"priorValues,omitempty"`

	// RemovedEntries: original key-value pairs that were deleted from the target
	// by the removals spec. Stored so they can be restored on revert.
	// +optional
	RemovedEntries NestedObject `json:"removedEntries,omitempty"`
}

// PatchRuleStatus reports the observed state.
type PatchRuleStatus struct {
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
	// +optional
	TargetCount int32 `json:"targetCount,omitempty"`

	// Conditions represent the latest available observations of the PatchRule's state.
	// +optional
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// Targets maps "namespace/name" (or just "name" for cluster-scoped) to the
	// state that was applied. Used for reverting patches on deletion or spec change.
	// +optional
	Targets map[string]TargetState `json:"targets,omitempty"`
}

// GetConditions returns the status conditions (implements the conditions accessor interface).
func (r *PatchRule) GetConditions() []metav1.Condition {
	return r.Status.Conditions
}

// SetConditions sets the status conditions (implements the conditions accessor interface).
func (r *PatchRule) SetConditions(conditions []metav1.Condition) {
	r.Status.Conditions = conditions
}

// +kubebuilder:object:root=true

// PatchRuleList is a list of PatchRule resources.
type PatchRuleList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`

	Items []PatchRule `json:"items"`
}
