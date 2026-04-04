package controller

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	patchworkv1 "github.com/andreasgerner/patchwork/api/v1"
)

var _ = Describe("PatchRule Controller", func() {
	const (
		namespace = "default"
		ruleName  = "test-rule"
	)

	Context("When creating a PatchRule targeting ConfigMaps", func() {
		var rule *patchworkv1.PatchRule

		BeforeEach(func() {
			// Create a target ConfigMap
			cm := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-configmap",
					Namespace: namespace,
					Labels:    map[string]string{"app": "test"},
				},
				Data: map[string]string{"existing": "value"},
			}
			Expect(k8sClient.Create(ctx, cm)).To(Succeed())

			// Create a PatchRule
			priority := int32(1)
			rule = &patchworkv1.PatchRule{
				ObjectMeta: metav1.ObjectMeta{
					Name:      ruleName,
					Namespace: namespace,
				},
				Spec: patchworkv1.PatchRuleSpec{
					Target: patchworkv1.TargetRef{
						APIVersion: "v1",
						Kind:       "ConfigMap",
						Conditions: patchworkv1.NestedObject{
							Data: map[string]interface{}{
								"metadata": map[string]interface{}{
									"labels": map[string]interface{}{
										"app": "test",
									},
								},
							},
						},
					},
					Priority:  &priority,
					Overwrite: true,
					Additions: patchworkv1.NestedObject{
						Data: map[string]interface{}{
							"metadata": map[string]interface{}{
								"labels": map[string]interface{}{
									"managed-by": "patchwork",
								},
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, rule)).To(Succeed())
		})

		AfterEach(func() {
			// Clean up
			_ = k8sClient.Delete(ctx, &patchworkv1.PatchRule{
				ObjectMeta: metav1.ObjectMeta{Name: ruleName, Namespace: namespace},
			})
			_ = k8sClient.Delete(ctx, &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{Name: "test-configmap", Namespace: namespace},
			})
		})

		It("should be created with the correct spec", func() {
			var fetched patchworkv1.PatchRule
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name:      ruleName,
				Namespace: namespace,
			}, &fetched)).To(Succeed())

			Expect(fetched.Spec.Target.Kind).To(Equal("ConfigMap"))
			Expect(fetched.Spec.Target.APIVersion).To(Equal("v1"))
			Expect(*fetched.Spec.Priority).To(Equal(int32(1)))
			Expect(fetched.Spec.Overwrite).To(BeTrue())
		})

		It("should accept priority 0", func() {
			zero := int32(0)
			lowRule := &patchworkv1.PatchRule{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "low-priority-rule",
					Namespace: namespace,
				},
				Spec: patchworkv1.PatchRuleSpec{
					Target: patchworkv1.TargetRef{
						APIVersion: "v1",
						Kind:       "ConfigMap",
					},
					Priority: &zero,
				},
			}
			Expect(k8sClient.Create(ctx, lowRule)).To(Succeed())

			var fetched patchworkv1.PatchRule
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(lowRule), &fetched)).To(Succeed())
			Expect(*fetched.Spec.Priority).To(Equal(int32(0)))

			Expect(k8sClient.Delete(ctx, lowRule)).To(Succeed())
		})
	})

	Context("matches function", func() {
		It("should match when conditions are met", func() {
			target := map[string]interface{}{
				"metadata": map[string]interface{}{
					"labels": map[string]interface{}{
						"app": "test",
					},
				},
			}
			conditions := map[string]interface{}{
				"metadata": map[string]interface{}{
					"labels": map[string]interface{}{
						"app": "test",
					},
				},
			}
			Expect(matches(target, conditions)).To(BeTrue())
		})

		It("should not match when conditions are not met", func() {
			target := map[string]interface{}{
				"metadata": map[string]interface{}{
					"labels": map[string]interface{}{
						"app": "other",
					},
				},
			}
			conditions := map[string]interface{}{
				"metadata": map[string]interface{}{
					"labels": map[string]interface{}{
						"app": "test",
					},
				},
			}
			Expect(matches(target, conditions)).To(BeFalse())
		})

		It("should match when conditions are nil", func() {
			target := map[string]interface{}{"metadata": map[string]interface{}{}}
			Expect(matches(target, nil)).To(BeTrue())
		})
	})

	Context("hasPriority function", func() {
		It("should prefer higher priority", func() {
			now := metav1.Now()
			high := int32(10)
			low := int32(1)
			a := &patchworkv1.PatchRule{
				ObjectMeta: metav1.ObjectMeta{Name: "a", CreationTimestamp: now},
				Spec:       patchworkv1.PatchRuleSpec{Priority: &high},
			}
			b := &patchworkv1.PatchRule{
				ObjectMeta: metav1.ObjectMeta{Name: "b", CreationTimestamp: now},
				Spec:       patchworkv1.PatchRuleSpec{Priority: &low},
			}
			Expect(hasPriority(a, b)).To(BeTrue())
			Expect(hasPriority(b, a)).To(BeFalse())
		})

		It("should fall back to creation time when priority is equal", func() {
			now := metav1.Now()
			later := metav1.NewTime(now.Add(1))
			pri := int32(1)
			a := &patchworkv1.PatchRule{
				ObjectMeta: metav1.ObjectMeta{Name: "a", CreationTimestamp: now},
				Spec:       patchworkv1.PatchRuleSpec{Priority: &pri},
			}
			b := &patchworkv1.PatchRule{
				ObjectMeta: metav1.ObjectMeta{Name: "b", CreationTimestamp: later},
				Spec:       patchworkv1.PatchRuleSpec{Priority: &pri},
			}
			Expect(hasPriority(a, b)).To(BeTrue())
			Expect(hasPriority(b, a)).To(BeFalse())
		})

		It("should fall back to name when priority and time are equal", func() {
			now := metav1.Now()
			pri := int32(1)
			a := &patchworkv1.PatchRule{
				ObjectMeta: metav1.ObjectMeta{Name: "aaa", CreationTimestamp: now},
				Spec:       patchworkv1.PatchRuleSpec{Priority: &pri},
			}
			b := &patchworkv1.PatchRule{
				ObjectMeta: metav1.ObjectMeta{Name: "zzz", CreationTimestamp: now},
				Spec:       patchworkv1.PatchRuleSpec{Priority: &pri},
			}
			Expect(hasPriority(a, b)).To(BeTrue())
			Expect(hasPriority(b, a)).To(BeFalse())
		})
	})

	Context("getPriority function", func() {
		It("should return the specified priority", func() {
			pri := int32(5)
			rule := &patchworkv1.PatchRule{Spec: patchworkv1.PatchRuleSpec{Priority: &pri}}
			Expect(getPriority(rule)).To(Equal(int32(5)))
		})

		It("should default to 1 when nil", func() {
			rule := &patchworkv1.PatchRule{}
			Expect(getPriority(rule)).To(Equal(int32(1)))
		})
	})
})

var _ = Describe("Patcher functions", func() {
	Context("buildPatch", func() {
		It("should return nil when nothing needs changing", func() {
			// Already has the desired value
			Expect(buildPatch(nil, nil)).To(BeNil())
		})
	})

	Context("collectAdditionPaths", func() {
		It("should collect leaf paths", func() {
			data := map[string]interface{}{
				"metadata": map[string]interface{}{
					"annotations": map[string]interface{}{
						"foo": "bar",
					},
					"labels": map[string]interface{}{
						"app": "test",
					},
				},
			}
			paths := collectAdditionPaths(data, "")
			Expect(paths).To(ConsistOf(
				"metadata.annotations.foo",
				"metadata.labels.app",
			))
		})

		It("should return nil for empty data", func() {
			Expect(collectAdditionPaths(nil, "")).To(BeEmpty())
		})
	})

	Context("collectRemovalPaths", func() {
		It("should collect removal leaf paths", func() {
			data := map[string]interface{}{
				"metadata": map[string]interface{}{
					"annotations": []interface{}{"deprecated", "old"},
				},
			}
			paths := collectRemovalPaths(data, "")
			Expect(paths).To(ConsistOf(
				"metadata.annotations.deprecated",
				"metadata.annotations.old",
			))
		})
	})

	Context("mergePriorValues", func() {
		It("should keep old values over new", func() {
			old := map[string]interface{}{"key": "original"}
			captured := map[string]interface{}{"key": "patched"}
			merged := mergePriorValues(old, captured)
			Expect(merged["key"]).To(Equal("original"))
		})

		It("should add new keys from captured", func() {
			old := map[string]interface{}{"a": "1"}
			captured := map[string]interface{}{"b": "2"}
			merged := mergePriorValues(old, captured)
			Expect(merged).To(HaveKeyWithValue("a", "1"))
			Expect(merged).To(HaveKeyWithValue("b", "2"))
		})

		It("should return other when one is nil", func() {
			data := map[string]interface{}{"key": "val"}
			Expect(mergePriorValues(nil, data)).To(Equal(data))
			Expect(mergePriorValues(data, nil)).To(Equal(data))
		})
	})

	Context("buildRevertPatch", func() {
		It("should restore prior values and delete added keys", func() {
			applied := map[string]interface{}{"added": "new", "changed": "new"}
			prior := map[string]interface{}{"changed": "original"}
			patch := buildRevertPatch(applied, prior, nil)
			Expect(patch["changed"]).To(Equal("original"))
			Expect(patch["added"]).To(BeNil()) // nil = JSON merge patch delete
			Expect(patch).To(HaveKey("added"))
		})

		It("should restore removed entries", func() {
			removed := map[string]interface{}{"key": "old-value"}
			patch := buildRevertPatch(nil, nil, removed)
			Expect(patch["key"]).To(Equal("old-value"))
		})

		It("should return nil when nothing to revert", func() {
			Expect(buildRevertPatch(nil, nil, nil)).To(BeNil())
		})
	})
})
