package patcher

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	patchworkv1 "github.com/andreasgerner/patchwork/api/v1"
)

// ---------------------------------------------------------------------------
// Matches
// ---------------------------------------------------------------------------

var _ = Describe("Matches", func() {
	It("matches when conditions are met", func() {
		got := map[string]interface{}{
			"metadata": map[string]interface{}{"labels": map[string]interface{}{"app": "test", "env": "prod"}},
		}
		want := map[string]interface{}{
			"metadata": map[string]interface{}{"labels": map[string]interface{}{"app": "test"}},
		}
		Expect(Matches(got, want)).To(BeTrue())
	})
	It("does not match when value differs", func() {
		Expect(Matches(
			map[string]interface{}{"metadata": map[string]interface{}{"labels": map[string]interface{}{"app": "other"}}},
			map[string]interface{}{"metadata": map[string]interface{}{"labels": map[string]interface{}{"app": "test"}}},
		)).To(BeFalse())
	})
	It("does not match when key is missing from got", func() {
		Expect(Matches(
			map[string]interface{}{"metadata": map[string]interface{}{}},
			map[string]interface{}{"metadata": map[string]interface{}{"labels": map[string]interface{}{"app": "test"}}},
		)).To(BeFalse())
	})
	It("always matches when want is nil", func() {
		Expect(Matches(map[string]interface{}{"a": "b"}, nil)).To(BeTrue())
	})
	It("always matches when want is empty", func() {
		Expect(Matches(map[string]interface{}{"a": "b"}, map[string]interface{}{})).To(BeTrue())
	})
	It("returns false for non-string non-map want value", func() {
		Expect(Matches(map[string]interface{}{"count": "3"}, map[string]interface{}{"count": 3})).To(BeFalse())
	})
	It("returns false when got has string but want expects map", func() {
		Expect(Matches(
			map[string]interface{}{"metadata": "flat"},
			map[string]interface{}{"metadata": map[string]interface{}{"labels": map[string]interface{}{}}},
		)).To(BeFalse())
	})
	It("returns false when got is nil and want is non-empty", func() {
		Expect(Matches(nil, map[string]interface{}{"key": "val"})).To(BeFalse())
	})
	It("returns true when both are nil", func() {
		Expect(Matches(nil, nil)).To(BeTrue())
	})
})

// ---------------------------------------------------------------------------
// setDefaults
// ---------------------------------------------------------------------------

var _ = Describe("setDefaults", func() {
	It("adds missing keys", func() {
		patch := map[string]interface{}{}
		setDefaults(map[string]interface{}{}, map[string]interface{}{"key": "val"}, patch, false)
		Expect(patch).To(HaveKeyWithValue("key", "val"))
	})
	It("skips existing key when overwrite=false", func() {
		patch := map[string]interface{}{}
		setDefaults(map[string]interface{}{"key": "old"}, map[string]interface{}{"key": "new"}, patch, false)
		Expect(patch).To(BeEmpty())
	})
	It("overwrites when overwrite=true", func() {
		patch := map[string]interface{}{}
		setDefaults(map[string]interface{}{"key": "old"}, map[string]interface{}{"key": "new"}, patch, true)
		Expect(patch).To(HaveKeyWithValue("key", "new"))
	})
	It("skips when value already matches", func() {
		patch := map[string]interface{}{}
		setDefaults(map[string]interface{}{"key": "val"}, map[string]interface{}{"key": "val"}, patch, true)
		Expect(patch).To(BeEmpty())
	})
	It("recurses into nested maps", func() {
		patch := map[string]interface{}{}
		setDefaults(
			map[string]interface{}{"metadata": map[string]interface{}{"labels": map[string]interface{}{}}},
			map[string]interface{}{"metadata": map[string]interface{}{"labels": map[string]interface{}{"app": "test"}}},
			patch, false,
		)
		labels := patch["metadata"].(map[string]interface{})["labels"].(map[string]interface{})
		Expect(labels).To(HaveKeyWithValue("app", "test"))
	})
	It("creates nested structure when target has no child", func() {
		patch := map[string]interface{}{}
		setDefaults(map[string]interface{}{}, map[string]interface{}{
			"metadata": map[string]interface{}{"labels": map[string]interface{}{"app": "test"}},
		}, patch, false)
		Expect(patch).To(HaveKey("metadata"))
	})
	It("cleans up empty sub-patches", func() {
		patch := map[string]interface{}{}
		setDefaults(
			map[string]interface{}{"metadata": map[string]interface{}{"labels": map[string]interface{}{"app": "test"}}},
			map[string]interface{}{"metadata": map[string]interface{}{"labels": map[string]interface{}{"app": "test"}}},
			patch, true,
		)
		Expect(patch).To(BeEmpty())
	})
})

// ---------------------------------------------------------------------------
// applyDeletes
// ---------------------------------------------------------------------------

var _ = Describe("applyDeletes", func() {
	It("sets matching keys to nil", func() {
		patch := map[string]interface{}{}
		applyDeletes(
			map[string]interface{}{"metadata": map[string]interface{}{"annotations": map[string]interface{}{"foo": "bar", "baz": "qux"}}},
			map[string]interface{}{"metadata": map[string]interface{}{"annotations": []interface{}{"foo"}}},
			patch,
		)
		ann := patch["metadata"].(map[string]interface{})["annotations"].(map[string]interface{})
		Expect(ann["foo"]).To(BeNil())
		Expect(ann).NotTo(HaveKey("baz"))
	})
	It("skips when target child is nil", func() {
		patch := map[string]interface{}{}
		applyDeletes(map[string]interface{}{}, map[string]interface{}{"metadata": map[string]interface{}{"annotations": []interface{}{"foo"}}}, patch)
		Expect(patch).To(BeEmpty())
	})
	It("skips when key does not exist in target", func() {
		patch := map[string]interface{}{}
		applyDeletes(
			map[string]interface{}{"metadata": map[string]interface{}{"annotations": map[string]interface{}{"other": "val"}}},
			map[string]interface{}{"metadata": map[string]interface{}{"annotations": []interface{}{"missing"}}},
			patch,
		)
		Expect(patch).To(BeEmpty())
	})
	It("skips non-string items in removal array", func() {
		patch := map[string]interface{}{}
		applyDeletes(
			map[string]interface{}{"metadata": map[string]interface{}{"annotations": map[string]interface{}{"foo": "bar"}}},
			map[string]interface{}{"metadata": map[string]interface{}{"annotations": []interface{}{123, true}}},
			patch,
		)
		Expect(patch).To(BeEmpty())
	})
})

// ---------------------------------------------------------------------------
// EnsureChild
// ---------------------------------------------------------------------------

var _ = Describe("ensureChild", func() {
	It("returns existing child map", func() {
		existing := map[string]interface{}{"key": "val"}
		Expect(ensureChild(map[string]interface{}{"child": existing}, "child")).To(Equal(existing))
	})
	It("creates new child when missing", func() {
		patch := map[string]interface{}{}
		Expect(ensureChild(patch, "child")).NotTo(BeNil())
		Expect(patch).To(HaveKey("child"))
	})
	It("overwrites non-map value with new map", func() {
		patch := map[string]interface{}{"child": "string"}
		ensureChild(patch, "child")
		_, isMap := patch["child"].(map[string]interface{})
		Expect(isMap).To(BeTrue())
	})
})

// ---------------------------------------------------------------------------
// CaptureOverwrittenValues
// ---------------------------------------------------------------------------

var _ = Describe("CaptureOverwrittenValues", func() {
	It("captures value that will be overwritten", func() {
		Expect(CaptureOverwrittenValues(map[string]interface{}{"key": "old"}, map[string]interface{}{"key": "new"}, true)).
			To(HaveKeyWithValue("key", "old"))
	})
	It("returns nil when overwrite=false", func() {
		Expect(CaptureOverwrittenValues(map[string]interface{}{"key": "old"}, map[string]interface{}{"key": "new"}, false)).To(BeNil())
	})
	It("skips missing key", func() {
		Expect(CaptureOverwrittenValues(map[string]interface{}{}, map[string]interface{}{"key": "new"}, true)).To(BeNil())
	})
	It("skips same value", func() {
		Expect(CaptureOverwrittenValues(map[string]interface{}{"key": "same"}, map[string]interface{}{"key": "same"}, true)).To(BeNil())
	})
	It("recurses into nested maps", func() {
		result := CaptureOverwrittenValues(
			map[string]interface{}{"metadata": map[string]interface{}{"labels": map[string]interface{}{"app": "old"}}},
			map[string]interface{}{"metadata": map[string]interface{}{"labels": map[string]interface{}{"app": "new"}}},
			true,
		)
		labels := result["metadata"].(map[string]interface{})["labels"].(map[string]interface{})
		Expect(labels).To(HaveKeyWithValue("app", "old"))
	})
	It("returns nil for nil inputs", func() {
		Expect(CaptureOverwrittenValues(nil, nil, true)).To(BeNil())
	})
})

// ---------------------------------------------------------------------------
// CaptureRemovedEntries
// ---------------------------------------------------------------------------

var _ = Describe("CaptureRemovedEntries", func() {
	It("captures values of keys to be removed", func() {
		result := CaptureRemovedEntries(
			map[string]interface{}{"metadata": map[string]interface{}{"annotations": map[string]interface{}{"foo": "bar", "baz": "qux"}}},
			map[string]interface{}{"metadata": map[string]interface{}{"annotations": []interface{}{"foo"}}},
		)
		Expect(result["metadata"].(map[string]interface{})).To(HaveKeyWithValue("foo", "bar"))
	})
	It("skips when target child is nil", func() {
		Expect(CaptureRemovedEntries(map[string]interface{}{}, map[string]interface{}{
			"metadata": map[string]interface{}{"annotations": []interface{}{"foo"}},
		})).To(BeNil())
	})
	It("skips when removal key does not exist in target", func() {
		Expect(CaptureRemovedEntries(
			map[string]interface{}{"metadata": map[string]interface{}{"annotations": map[string]interface{}{"other": "val"}}},
			map[string]interface{}{"metadata": map[string]interface{}{"annotations": []interface{}{"missing"}}},
		)).To(BeNil())
	})
	It("returns nil for nil inputs", func() {
		Expect(CaptureRemovedEntries(nil, nil)).To(BeNil())
	})
})

// ---------------------------------------------------------------------------
// CaptureAppliedAdditions
// ---------------------------------------------------------------------------

var _ = Describe("CaptureAppliedAdditions", func() {
	It("includes all when overwrite=true", func() {
		result := CaptureAppliedAdditions(map[string]interface{}{"key": "old"}, map[string]interface{}{"key": "new", "extra": "val"}, true)
		Expect(result).To(HaveKeyWithValue("key", "new"))
		Expect(result).To(HaveKeyWithValue("extra", "val"))
	})
	It("skips non-overwritten existing key when overwrite=false", func() {
		Expect(CaptureAppliedAdditions(map[string]interface{}{"key": "different"}, map[string]interface{}{"key": "new"}, false)).To(BeNil())
	})
	It("includes when overwrite=false and value matches", func() {
		Expect(CaptureAppliedAdditions(map[string]interface{}{"key": "new"}, map[string]interface{}{"key": "new"}, false)).
			To(HaveKeyWithValue("key", "new"))
	})
	It("includes when overwrite=false and key absent", func() {
		Expect(CaptureAppliedAdditions(map[string]interface{}{}, map[string]interface{}{"key": "new"}, false)).
			To(HaveKeyWithValue("key", "new"))
	})
	It("returns nil for nil inputs", func() {
		Expect(CaptureAppliedAdditions(nil, nil, true)).To(BeNil())
	})
})

// ---------------------------------------------------------------------------
// BuildPatch
// ---------------------------------------------------------------------------

var _ = Describe("BuildPatch", func() {
	It("returns nil when nothing needs changing", func() {
		target := &unstructured.Unstructured{Object: map[string]interface{}{
			"metadata": map[string]interface{}{"labels": map[string]interface{}{"app": "test"}},
		}}
		rule := &patchworkv1.PatchRule{Spec: patchworkv1.PatchRuleSpec{
			Overwrite: true,
			Additions: patchworkv1.NestedObject{Data: map[string]interface{}{
				"metadata": map[string]interface{}{"labels": map[string]interface{}{"app": "test"}},
			}},
		}}
		Expect(BuildPatch(target, rule)).To(BeNil())
	})
	It("produces patch for additions", func() {
		target := &unstructured.Unstructured{Object: map[string]interface{}{}}
		rule := &patchworkv1.PatchRule{Spec: patchworkv1.PatchRuleSpec{
			Additions: patchworkv1.NestedObject{Data: map[string]interface{}{"key": "val"}},
		}}
		Expect(BuildPatch(target, rule)).To(HaveKeyWithValue("key", "val"))
	})
	It("produces patch for removals", func() {
		target := &unstructured.Unstructured{Object: map[string]interface{}{
			"metadata": map[string]interface{}{"annotations": map[string]interface{}{"old": "val"}},
		}}
		rule := &patchworkv1.PatchRule{Spec: patchworkv1.PatchRuleSpec{
			Removals: patchworkv1.NestedObject{Data: map[string]interface{}{
				"metadata": map[string]interface{}{"annotations": []interface{}{"old"}},
			}},
		}}
		Expect(BuildPatch(target, rule)).NotTo(BeNil())
	})
	It("handles both additions and removals", func() {
		target := &unstructured.Unstructured{Object: map[string]interface{}{
			"metadata": map[string]interface{}{"annotations": map[string]interface{}{"old": "val"}},
		}}
		rule := &patchworkv1.PatchRule{Spec: patchworkv1.PatchRuleSpec{
			Overwrite: true,
			Additions: patchworkv1.NestedObject{Data: map[string]interface{}{
				"metadata": map[string]interface{}{"labels": map[string]interface{}{"new": "label"}},
			}},
			Removals: patchworkv1.NestedObject{Data: map[string]interface{}{
				"metadata": map[string]interface{}{"annotations": []interface{}{"old"}},
			}},
		}}
		Expect(BuildPatch(target, rule)).To(HaveKey("metadata"))
	})
	It("returns nil for empty spec", func() {
		Expect(BuildPatch(&unstructured.Unstructured{Object: map[string]interface{}{}}, &patchworkv1.PatchRule{})).To(BeNil())
	})
})

// ---------------------------------------------------------------------------
// BuildRevertPatch
// ---------------------------------------------------------------------------

var _ = Describe("BuildRevertPatch", func() {
	It("restores prior values and deletes added keys", func() {
		patch := BuildRevertPatch(map[string]interface{}{"added": "new", "changed": "new"}, map[string]interface{}{"changed": "original"}, nil)
		Expect(patch["changed"]).To(Equal("original"))
		Expect(patch).To(HaveKey("added"))
		Expect(patch["added"]).To(BeNil())
	})
	It("restores removed entries", func() {
		Expect(BuildRevertPatch(nil, nil, map[string]interface{}{"key": "old"})).To(HaveKeyWithValue("key", "old"))
	})
	It("returns nil when nothing to revert", func() {
		Expect(BuildRevertPatch(nil, nil, nil)).To(BeNil())
	})
	It("handles nested additions with mixed prior/no-prior", func() {
		patch := BuildRevertPatch(
			map[string]interface{}{"metadata": map[string]interface{}{"labels": map[string]interface{}{"added": "new", "changed": "new"}}},
			map[string]interface{}{"metadata": map[string]interface{}{"labels": map[string]interface{}{"changed": "original"}}},
			nil,
		)
		labels := patch["metadata"].(map[string]interface{})["labels"].(map[string]interface{})
		Expect(labels["changed"]).To(Equal("original"))
		Expect(labels["added"]).To(BeNil())
	})
	It("handles nested removed entries", func() {
		patch := BuildRevertPatch(nil, nil, map[string]interface{}{
			"metadata": map[string]interface{}{"annotations": map[string]interface{}{"foo": "bar"}},
		})
		ann := patch["metadata"].(map[string]interface{})["annotations"].(map[string]interface{})
		Expect(ann).To(HaveKeyWithValue("foo", "bar"))
	})
})

// ---------------------------------------------------------------------------
// BuildDiffRevertPatch
// ---------------------------------------------------------------------------

var _ = Describe("BuildDiffRevertPatch", func() {
	It("reverts dropped additions with prior", func() {
		patch := BuildDiffRevertPatch(
			map[string]interface{}{"kept": "val", "dropped": "val"},
			map[string]interface{}{"kept": "val"},
			map[string]interface{}{"dropped": "original"}, nil, nil,
		)
		Expect(patch).To(HaveKeyWithValue("dropped", "original"))
		Expect(patch).NotTo(HaveKey("kept"))
	})
	It("deletes added key with no prior when dropped", func() {
		patch := BuildDiffRevertPatch(map[string]interface{}{"added": "val"}, nil, nil, nil, nil)
		Expect(patch).To(HaveKey("added"))
		Expect(patch["added"]).To(BeNil())
	})
	It("restores dropped removal entries", func() {
		Expect(BuildDiffRevertPatch(nil, nil, nil, map[string]interface{}{"key": "restored"}, nil)).
			To(HaveKeyWithValue("key", "restored"))
	})
	It("returns nil when nothing changed", func() {
		same := map[string]interface{}{"key": "val"}
		Expect(BuildDiffRevertPatch(same, same, nil, nil, nil)).To(BeNil())
	})
	It("handles nested sub-key removal", func() {
		patch := BuildDiffRevertPatch(
			map[string]interface{}{"metadata": map[string]interface{}{"labels": map[string]interface{}{"a": "1", "b": "2"}}},
			map[string]interface{}{"metadata": map[string]interface{}{"labels": map[string]interface{}{"a": "1"}}},
			nil, nil, nil,
		)
		labels := patch["metadata"].(map[string]interface{})["labels"].(map[string]interface{})
		Expect(labels).To(HaveKey("b"))
		Expect(labels["b"]).To(BeNil())
	})
})

// ---------------------------------------------------------------------------
// CollectAdditionPaths / CollectRemovalPaths / CollectTrackedPaths
// ---------------------------------------------------------------------------

var _ = Describe("CollectAdditionPaths", func() {
	It("collects nested leaf paths", func() {
		Expect(CollectAdditionPaths(map[string]interface{}{
			"metadata": map[string]interface{}{
				"annotations": map[string]interface{}{"foo": "bar"},
				"labels":      map[string]interface{}{"app": "test"},
			},
		}, "")).To(ConsistOf("metadata.annotations.foo", "metadata.labels.app"))
	})
	It("handles flat key", func() {
		Expect(CollectAdditionPaths(map[string]interface{}{"key": "val"}, "")).To(ConsistOf("key"))
	})
	It("respects prefix", func() {
		Expect(CollectAdditionPaths(map[string]interface{}{"key": "val"}, "root")).To(ConsistOf("root.key"))
	})
	It("returns empty for nil", func() {
		Expect(CollectAdditionPaths(nil, "")).To(BeEmpty())
	})
	It("treats non-string non-map as leaf", func() {
		Expect(CollectAdditionPaths(map[string]interface{}{"count": 42}, "")).To(ConsistOf("count"))
	})
})

var _ = Describe("CollectRemovalPaths", func() {
	It("collects paths from string arrays", func() {
		Expect(CollectRemovalPaths(map[string]interface{}{
			"metadata": map[string]interface{}{"annotations": []interface{}{"deprecated", "old"}},
		}, "")).To(ConsistOf("metadata.annotations.deprecated", "metadata.annotations.old"))
	})
	It("returns empty for nil", func() {
		Expect(CollectRemovalPaths(nil, "")).To(BeEmpty())
	})
	It("skips non-string items", func() {
		Expect(CollectRemovalPaths(map[string]interface{}{
			"metadata": map[string]interface{}{"annotations": []interface{}{123, "valid"}},
		}, "")).To(ConsistOf("metadata.annotations.valid"))
	})
	It("returns empty for empty array", func() {
		Expect(CollectRemovalPaths(map[string]interface{}{
			"metadata": map[string]interface{}{"annotations": []interface{}{}},
		}, "")).To(BeEmpty())
	})
	It("respects prefix", func() {
		Expect(CollectRemovalPaths(map[string]interface{}{"keys": []interface{}{"a"}}, "root")).To(ConsistOf("root.keys.a"))
	})
})

var _ = Describe("CollectTrackedPaths", func() {
	It("collects from both additions and removed entries", func() {
		Expect(CollectTrackedPaths(patchworkv1.TargetState{
			AppliedAdditions: patchworkv1.NestedObject{Data: map[string]interface{}{"a": "1"}},
			RemovedEntries:   patchworkv1.NestedObject{Data: map[string]interface{}{"b": "2"}},
		})).To(ConsistOf("a", "b"))
	})
	It("returns empty for empty state", func() {
		Expect(CollectTrackedPaths(patchworkv1.TargetState{})).To(BeEmpty())
	})
})

// ---------------------------------------------------------------------------
// MergePriorValues
// ---------------------------------------------------------------------------

var _ = Describe("MergePriorValues", func() {
	It("old wins over captured", func() {
		Expect(MergePriorValues(map[string]interface{}{"key": "original"}, map[string]interface{}{"key": "patched"})).
			To(HaveKeyWithValue("key", "original"))
	})
	It("merges disjoint keys", func() {
		result := MergePriorValues(map[string]interface{}{"a": "1"}, map[string]interface{}{"b": "2"})
		Expect(result).To(HaveKeyWithValue("a", "1"))
		Expect(result).To(HaveKeyWithValue("b", "2"))
	})
	It("returns captured when old is nil", func() {
		Expect(MergePriorValues(nil, map[string]interface{}{"a": "1"})).To(HaveKeyWithValue("a", "1"))
	})
	It("returns old when captured is nil", func() {
		Expect(MergePriorValues(map[string]interface{}{"a": "1"}, nil)).To(HaveKeyWithValue("a", "1"))
	})
	It("returns nil when both nil", func() {
		Expect(MergePriorValues(nil, nil)).To(BeNil())
	})
	It("recursively merges nested maps", func() {
		result := MergePriorValues(
			map[string]interface{}{"metadata": map[string]interface{}{"labels": map[string]interface{}{"a": "original"}}},
			map[string]interface{}{"metadata": map[string]interface{}{"labels": map[string]interface{}{"a": "patched", "b": "new"}}},
		)
		labels := result["metadata"].(map[string]interface{})["labels"].(map[string]interface{})
		Expect(labels).To(HaveKeyWithValue("a", "original"))
		Expect(labels).To(HaveKeyWithValue("b", "new"))
	})
	It("old non-map overwrites captured map", func() {
		result := MergePriorValues(
			map[string]interface{}{"key": "flat"},
			map[string]interface{}{"key": map[string]interface{}{"nested": "val"}},
		)
		Expect(result["key"]).To(Equal("flat"))
	})
})
