package v1

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

// controller-gen generates DeepCopy for root types (PatchRule, PatchRuleList)
// but not for sub-types that contain map[string]interface{}. These must be
// written manually.

// DeepCopyInto copies all fields into the given PatchRuleSpec.
func (in *PatchRuleSpec) DeepCopyInto(out *PatchRuleSpec) {
	*out = *in
	in.Target.DeepCopyInto(&out.Target)
	if in.Priority != nil {
		p := *in.Priority
		out.Priority = &p
	}
	out.Additions = in.Additions.DeepCopy()
	out.Removals = in.Removals.DeepCopy()
}

// DeepCopy returns a deep copy of the PatchRuleSpec.
func (in *PatchRuleSpec) DeepCopy() *PatchRuleSpec {
	if in == nil {
		return nil
	}
	out := new(PatchRuleSpec)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto copies all fields into the given TargetRef.
func (in *TargetRef) DeepCopyInto(out *TargetRef) {
	*out = *in
	out.Conditions = in.Conditions.DeepCopy()
}

// DeepCopy returns a deep copy of the TargetRef.
func (in *TargetRef) DeepCopy() *TargetRef {
	if in == nil {
		return nil
	}
	out := new(TargetRef)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto copies all fields into the given TargetState.
func (in *TargetState) DeepCopyInto(out *TargetState) {
	*out = *in
	out.AppliedAdditions = in.AppliedAdditions.DeepCopy()
	out.PriorValues = in.PriorValues.DeepCopy()
	out.RemovedEntries = in.RemovedEntries.DeepCopy()
}

// DeepCopy returns a deep copy of the TargetState.
func (in *TargetState) DeepCopy() *TargetState {
	if in == nil {
		return nil
	}
	out := new(TargetState)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto copies all fields into the given PatchRuleStatus.
func (in *PatchRuleStatus) DeepCopyInto(out *PatchRuleStatus) {
	*out = *in
	if in.Conditions != nil {
		out.Conditions = make([]metav1.Condition, len(in.Conditions))
		for i := range in.Conditions {
			in.Conditions[i].DeepCopyInto(&out.Conditions[i])
		}
	}
	if in.Targets != nil {
		out.Targets = make(map[string]TargetState, len(in.Targets))
		for k, v := range in.Targets {
			out.Targets[k] = *v.DeepCopy()
		}
	}
}

// DeepCopy returns a deep copy of the PatchRuleStatus.
func (in *PatchRuleStatus) DeepCopy() *PatchRuleStatus {
	if in == nil {
		return nil
	}
	out := new(PatchRuleStatus)
	in.DeepCopyInto(out)
	return out
}
