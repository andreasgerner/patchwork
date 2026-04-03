package v1

// controller-gen generates DeepCopy for root types (PatchRule, PatchRuleList)
// but not for sub-types that contain map[string]interface{}. These must be
// written manually.

func (in *PatchRuleSpec) DeepCopyInto(out *PatchRuleSpec) {
	*out = *in
	in.Target.DeepCopyInto(&out.Target)
	out.Additions = in.Additions.DeepCopy()
	out.Removals = in.Removals.DeepCopy()
}

func (in *PatchRuleSpec) DeepCopy() *PatchRuleSpec {
	if in == nil {
		return nil
	}
	out := new(PatchRuleSpec)
	in.DeepCopyInto(out)
	return out
}

func (in *TargetRef) DeepCopyInto(out *TargetRef) {
	*out = *in
	out.Conditions = in.Conditions.DeepCopy()
}

func (in *TargetRef) DeepCopy() *TargetRef {
	if in == nil {
		return nil
	}
	out := new(TargetRef)
	in.DeepCopyInto(out)
	return out
}
