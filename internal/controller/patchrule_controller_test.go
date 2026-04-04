package controller

import (
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	patchworkv1 "github.com/andreasgerner/patchwork/api/v1"
)

// ---------------------------------------------------------------------------
// Integration tests (require envtest)
// ---------------------------------------------------------------------------

var _ = Describe("PatchRule Controller", func() {
	const namespace = "default"

	Context("When creating a PatchRule targeting ConfigMaps", func() {
		var rule *patchworkv1.PatchRule

		BeforeEach(func() {
			cm := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-configmap",
					Namespace: namespace,
					Labels:    map[string]string{"app": "test"},
				},
				Data: map[string]string{"existing": "value"},
			}
			Expect(k8sClient.Create(ctx, cm)).To(Succeed())

			priority := int32(1)
			rule = &patchworkv1.PatchRule{
				ObjectMeta: metav1.ObjectMeta{Name: "test-rule", Namespace: namespace},
				Spec: patchworkv1.PatchRuleSpec{
					Target: patchworkv1.TargetRef{
						APIVersion: "v1",
						Kind:       "ConfigMap",
						Conditions: patchworkv1.NestedObject{Data: map[string]interface{}{
							"metadata": map[string]interface{}{"labels": map[string]interface{}{"app": "test"}},
						}},
					},
					Priority:  &priority,
					Overwrite: true,
					Additions: patchworkv1.NestedObject{Data: map[string]interface{}{
						"metadata": map[string]interface{}{"labels": map[string]interface{}{"managed-by": "patchwork"}},
					}},
				},
			}
			Expect(k8sClient.Create(ctx, rule)).To(Succeed())
		})

		AfterEach(func() {
			_ = k8sClient.Delete(ctx, &patchworkv1.PatchRule{ObjectMeta: metav1.ObjectMeta{Name: "test-rule", Namespace: namespace}})
			_ = k8sClient.Delete(ctx, &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "test-configmap", Namespace: namespace}})
		})

		It("should be created with the correct spec", func() {
			var fetched patchworkv1.PatchRule
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "test-rule", Namespace: namespace}, &fetched)).To(Succeed())
			Expect(fetched.Spec.Target.Kind).To(Equal("ConfigMap"))
			Expect(fetched.Spec.Target.APIVersion).To(Equal("v1"))
			Expect(*fetched.Spec.Priority).To(Equal(int32(1)))
			Expect(fetched.Spec.Overwrite).To(BeTrue())
		})

		It("should accept priority 0", func() {
			zero := int32(0)
			r := &patchworkv1.PatchRule{
				ObjectMeta: metav1.ObjectMeta{Name: "low-pri", Namespace: namespace},
				Spec:       patchworkv1.PatchRuleSpec{Target: patchworkv1.TargetRef{APIVersion: "v1", Kind: "ConfigMap"}, Priority: &zero},
			}
			Expect(k8sClient.Create(ctx, r)).To(Succeed())
			var fetched patchworkv1.PatchRule
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(r), &fetched)).To(Succeed())
			Expect(*fetched.Spec.Priority).To(Equal(int32(0)))
			Expect(k8sClient.Delete(ctx, r)).To(Succeed())
		})
	})
})

// ---------------------------------------------------------------------------
// hasPriority / getPriority
// ---------------------------------------------------------------------------

var _ = Describe("hasPriority", func() {
	now := metav1.Now()
	later := metav1.NewTime(now.Add(time.Second))
	pri := func(p int32) *int32 { return &p }

	It("higher priority wins", func() {
		a := &patchworkv1.PatchRule{ObjectMeta: metav1.ObjectMeta{Name: "a", CreationTimestamp: now}, Spec: patchworkv1.PatchRuleSpec{Priority: pri(10)}}
		b := &patchworkv1.PatchRule{ObjectMeta: metav1.ObjectMeta{Name: "b", CreationTimestamp: now}, Spec: patchworkv1.PatchRuleSpec{Priority: pri(1)}}
		Expect(hasPriority(a, b)).To(BeTrue())
		Expect(hasPriority(b, a)).To(BeFalse())
	})

	It("equal priority falls back to creation time", func() {
		a := &patchworkv1.PatchRule{ObjectMeta: metav1.ObjectMeta{Name: "a", CreationTimestamp: now}, Spec: patchworkv1.PatchRuleSpec{Priority: pri(1)}}
		b := &patchworkv1.PatchRule{ObjectMeta: metav1.ObjectMeta{Name: "b", CreationTimestamp: later}, Spec: patchworkv1.PatchRuleSpec{Priority: pri(1)}}
		Expect(hasPriority(a, b)).To(BeTrue())
		Expect(hasPriority(b, a)).To(BeFalse())
	})

	It("equal priority and time falls back to name", func() {
		a := &patchworkv1.PatchRule{ObjectMeta: metav1.ObjectMeta{Name: "aaa", CreationTimestamp: now}, Spec: patchworkv1.PatchRuleSpec{Priority: pri(1)}}
		b := &patchworkv1.PatchRule{ObjectMeta: metav1.ObjectMeta{Name: "zzz", CreationTimestamp: now}, Spec: patchworkv1.PatchRuleSpec{Priority: pri(1)}}
		Expect(hasPriority(a, b)).To(BeTrue())
		Expect(hasPriority(b, a)).To(BeFalse())
	})

	It("nil priority defaults to 1", func() {
		a := &patchworkv1.PatchRule{ObjectMeta: metav1.ObjectMeta{Name: "a", CreationTimestamp: now}, Spec: patchworkv1.PatchRuleSpec{Priority: pri(2)}}
		b := &patchworkv1.PatchRule{ObjectMeta: metav1.ObjectMeta{Name: "b", CreationTimestamp: now}}
		Expect(hasPriority(a, b)).To(BeTrue())
	})

	It("both nil priorities are equal (falls back to name)", func() {
		a := &patchworkv1.PatchRule{ObjectMeta: metav1.ObjectMeta{Name: "a", CreationTimestamp: now}}
		b := &patchworkv1.PatchRule{ObjectMeta: metav1.ObjectMeta{Name: "b", CreationTimestamp: now}}
		Expect(hasPriority(a, b)).To(BeTrue())
	})
})

var _ = Describe("getPriority", func() {
	It("returns the set value", func() {
		p := int32(5)
		Expect(getPriority(&patchworkv1.PatchRule{Spec: patchworkv1.PatchRuleSpec{Priority: &p}})).To(Equal(int32(5)))
	})
	It("returns 1 for nil", func() {
		Expect(getPriority(&patchworkv1.PatchRule{})).To(Equal(int32(1)))
	})
	It("returns 0 when set to 0", func() {
		p := int32(0)
		Expect(getPriority(&patchworkv1.PatchRule{Spec: patchworkv1.PatchRuleSpec{Priority: &p}})).To(Equal(int32(0)))
	})
})

// ---------------------------------------------------------------------------
// targetKey / parseTargetKey
// ---------------------------------------------------------------------------

var _ = Describe("targetKey / parseTargetKey", func() {
	It("roundtrips namespace/name", func() {
		key := targetKey("my-ns", "my-name")
		Expect(key).To(Equal("my-ns/my-name"))
		ns, name := parseTargetKey(key)
		Expect(ns).To(Equal("my-ns"))
		Expect(name).To(Equal("my-name"))
	})
	It("handles cluster-scoped (no namespace)", func() {
		key := targetKey("", "my-name")
		Expect(key).To(Equal("my-name"))
		ns, name := parseTargetKey(key)
		Expect(ns).To(Equal(""))
		Expect(name).To(Equal("my-name"))
	})
	It("handles name containing slash", func() {
		ns, name := parseTargetKey("ns/name/extra")
		Expect(ns).To(Equal("ns"))
		Expect(name).To(Equal("name/extra"))
	})
	It("handles empty key", func() {
		ns, name := parseTargetKey("")
		Expect(ns).To(Equal(""))
		Expect(name).To(Equal(""))
	})
})

// ---------------------------------------------------------------------------
// Status condition helpers
// ---------------------------------------------------------------------------

var _ = Describe("status condition helpers", func() {
	var rule *patchworkv1.PatchRule

	BeforeEach(func() {
		rule = &patchworkv1.PatchRule{ObjectMeta: metav1.ObjectMeta{Generation: 1}}
	})

	It("setReadyCondition sets Ready=True", func() {
		setReadyCondition(rule)
		Expect(rule.Status.Conditions).To(HaveLen(1))
		Expect(rule.Status.Conditions[0].Type).To(Equal(patchworkv1.ConditionReady))
		Expect(rule.Status.Conditions[0].Status).To(Equal(metav1.ConditionTrue))
		Expect(rule.Status.Conditions[0].ObservedGeneration).To(Equal(int64(1)))
	})

	It("setNotReadyCondition sets Ready=False with message", func() {
		setNotReadyCondition(rule, "some reason")
		Expect(rule.Status.Conditions).To(HaveLen(1))
		Expect(rule.Status.Conditions[0].Status).To(Equal(metav1.ConditionFalse))
		Expect(rule.Status.Conditions[0].Message).To(Equal("some reason"))
	})

	It("setConflictedCondition sets both Conflicted=True and Ready=False", func() {
		setConflictedCondition(rule, "conflict msg")
		Expect(rule.Status.Conditions).To(HaveLen(2))
		Expect(isConflicted(rule)).To(BeTrue())
		for _, c := range rule.Status.Conditions {
			if c.Type == patchworkv1.ConditionReady {
				Expect(c.Status).To(Equal(metav1.ConditionFalse))
			}
		}
	})

	It("clearConflictedCondition sets Conflicted=False", func() {
		setConflictedCondition(rule, "conflict")
		clearConflictedCondition(rule)
		Expect(isConflicted(rule)).To(BeFalse())
	})

	It("isConflicted returns false when no conditions", func() {
		Expect(isConflicted(rule)).To(BeFalse())
	})

	It("isConflicted returns false when Conflicted=False", func() {
		clearConflictedCondition(rule)
		Expect(isConflicted(rule)).To(BeFalse())
	})

	It("setReadyCondition is idempotent", func() {
		setReadyCondition(rule)
		setReadyCondition(rule)
		count := 0
		for _, c := range rule.Status.Conditions {
			if c.Type == patchworkv1.ConditionReady {
				count++
			}
		}
		Expect(count).To(Equal(1))
	})

	It("transitions from Ready=True to Ready=False", func() {
		setReadyCondition(rule)
		setNotReadyCondition(rule, "broken")
		for _, c := range rule.Status.Conditions {
			if c.Type == patchworkv1.ConditionReady {
				Expect(c.Status).To(Equal(metav1.ConditionFalse))
			}
		}
	})
})
