package controller

import (
	"context"
	"fmt"
	"sync"

	v1 "github.com/andreasgerner/patchwork/api/v1"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	kerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

// PatchRuleReconciler watches PatchRule CRs and patches matching target resources.
type PatchRuleReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder

	dynCtrl     controller.Controller
	cache       cache.Cache
	watchedGVRs map[schema.GroupVersionResource]bool
	mu          sync.Mutex
}

// +kubebuilder:rbac:groups=patchwork.io,resources=patchrules,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=patchwork.io,resources=patchrules/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=patchwork.io,resources=patchrules/finalizers,verbs=update
// +kubebuilder:rbac:groups="*",resources="*",verbs=get;list;watch;patch
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch

// SetupWithManager registers the controller with the manager.
func (r *PatchRuleReconciler) SetupWithManager(mgr ctrl.Manager) error {
	r.cache = mgr.GetCache()
	r.watchedGVRs = make(map[schema.GroupVersionResource]bool)

	c, err := ctrl.NewControllerManagedBy(mgr).
		For(&v1.PatchRule{}).
		// Re-enqueue conflicted siblings when any PatchRule changes spec (generation-gated).
		Watches(&v1.PatchRule{},
			handler.EnqueueRequestsFromMapFunc(r.enqueueSiblingRules),
			builder.WithPredicates(predicate.GenerationChangedPredicate{}),
		).
		Build(r)
	if err != nil {
		return err
	}
	r.dynCtrl = c
	return nil
}

// Reconcile dispatches to reconcileDelete or reconcileNormal, with a deferred
// status patch that always runs regardless of errors.
func (r *PatchRuleReconciler) Reconcile(ctx context.Context, req ctrl.Request) (retRes ctrl.Result, retErr error) {
	log := log.FromContext(ctx)

	var rule v1.PatchRule
	if err := r.Get(ctx, req.NamespacedName, &rule); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Snapshot for deferred status patch
	statusBase := rule.DeepCopy()

	// Always patch status at the end of reconcile
	defer func() {
		if err := r.Status().Patch(ctx, &rule, client.MergeFrom(statusBase)); err != nil {
			retErr = kerrors.NewAggregate([]error{retErr, err})
		}
	}()

	// Handle deletion
	if !rule.DeletionTimestamp.IsZero() {
		if controllerutil.ContainsFinalizer(&rule, v1.PatchRuleFinalizer) {
			log.Info("reverting all patches for deleted PatchRule")
			result, err := r.reconcileDelete(ctx, &rule)
			if err != nil {
				return result, err
			}
			controllerutil.RemoveFinalizer(&rule, v1.PatchRuleFinalizer)
			if err := r.Patch(ctx, &rule, client.MergeFrom(statusBase)); err != nil {
				return ctrl.Result{}, err
			}
		}
		return ctrl.Result{}, nil
	}

	// Ensure finalizer (via Patch, not Update)
	if !controllerutil.ContainsFinalizer(&rule, v1.PatchRuleFinalizer) {
		original := rule.DeepCopy()
		controllerutil.AddFinalizer(&rule, v1.PatchRuleFinalizer)
		if err := r.Patch(ctx, &rule, client.MergeFrom(original)); err != nil {
			return ctrl.Result{}, err
		}
	}

	return r.reconcileNormal(ctx, &rule)
}

// checkConflicts checks whether any higher-priority PatchRule already owns
// paths that this rule wants to set or remove on the same concrete targets.
func (r *PatchRuleReconciler) checkConflicts(
	ctx context.Context,
	rule *v1.PatchRule,
	gvk schema.GroupVersionKind,
	matchedTargetKeys []string,
) (conflictingRule string, overlappingPaths []string, err error) {
	if len(matchedTargetKeys) == 0 {
		return "", nil, nil
	}

	ourPaths := make(map[string]bool)
	for _, p := range collectAdditionPaths(rule.Spec.Additions.Data, "") {
		ourPaths[p] = true
	}
	for _, p := range collectRemovalPaths(rule.Spec.Removals.Data, "") {
		ourPaths[p] = true
	}
	if len(ourPaths) == 0 {
		return "", nil, nil
	}

	matchedSet := make(map[string]bool, len(matchedTargetKeys))
	for _, k := range matchedTargetKeys {
		matchedSet[k] = true
	}

	var list v1.PatchRuleList
	if err := r.List(ctx, &list); err != nil {
		return "", nil, err
	}

	for i := range list.Items {
		other := &list.Items[i]

		if other.UID == rule.UID {
			continue
		}
		otherGVK := schema.FromAPIVersionAndKind(other.Spec.Target.APIVersion, other.Spec.Target.Kind)
		if otherGVK != gvk {
			continue
		}
		if !other.DeletionTimestamp.IsZero() {
			continue
		}
		if !hasPriority(other, rule) {
			continue
		}
		if len(other.Status.Targets) == 0 {
			continue
		}

		for tk := range matchedSet {
			otherState, ok := other.Status.Targets[tk]
			if !ok {
				continue
			}
			var overlap []string
			for _, p := range collectTrackedPaths(otherState) {
				if ourPaths[p] {
					overlap = append(overlap, p)
				}
			}
			if len(overlap) > 0 {
				return other.Name, overlap, nil
			}
		}
	}

	return "", nil, nil
}

// hasPriority returns true if a has priority over b.
// Order: higher spec.priority wins, then earlier creationTimestamp, then lower name.
func hasPriority(a, b *v1.PatchRule) bool {
	aPri, bPri := getPriority(a), getPriority(b)
	if aPri != bPri {
		return aPri > bPri
	}
	if a.CreationTimestamp.Before(&b.CreationTimestamp) {
		return true
	}
	if b.CreationTimestamp.Before(&a.CreationTimestamp) {
		return false
	}
	return a.Name < b.Name
}

// getPriority returns the rule's priority, defaulting to 1 if unset.
func getPriority(rule *v1.PatchRule) int32 {
	if rule.Spec.Priority != nil {
		return *rule.Spec.Priority
	}
	return 1
}

// enqueueSiblingRules maps a PatchRule event to reconcile requests for all
// other PatchRules targeting the same GVK that are currently conflicted.
func (r *PatchRuleReconciler) enqueueSiblingRules(ctx context.Context, obj client.Object) []reconcile.Request {
	rule, ok := obj.(*v1.PatchRule)
	if !ok {
		return nil
	}

	gvk := schema.FromAPIVersionAndKind(rule.Spec.Target.APIVersion, rule.Spec.Target.Kind)

	var list v1.PatchRuleList
	if err := r.List(ctx, &list); err != nil {
		log.FromContext(ctx).Error(err, "list PatchRules for sibling enqueue")
		return nil
	}

	var reqs []reconcile.Request
	for _, other := range list.Items {
		if other.UID == rule.UID {
			continue
		}
		otherGVK := schema.FromAPIVersionAndKind(other.Spec.Target.APIVersion, other.Spec.Target.Kind)
		if otherGVK != gvk {
			continue
		}
		if !isConflicted(&other) {
			continue
		}
		reqs = append(reqs, reconcile.Request{
			NamespacedName: types.NamespacedName{Name: other.Name, Namespace: other.Namespace},
		})
	}
	return reqs
}

// ensureWatch starts a dynamic informer for the given resource type if we
// aren't already watching it.
func (r *PatchRuleReconciler) ensureWatch(gvr schema.GroupVersionResource, gvk schema.GroupVersionKind) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.watchedGVRs[gvr] {
		return nil
	}

	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(gvk)

	mapFn := handler.TypedEnqueueRequestsFromMapFunc[
		*unstructured.Unstructured, reconcile.Request,
	](r.findRulesFor(gvk))

	if err := r.dynCtrl.Watch(source.Kind(r.cache, obj, mapFn)); err != nil {
		return fmt.Errorf("watch %v: %w", gvr, err)
	}

	r.watchedGVRs[gvr] = true
	log.Log.Info("watching", "gvr", gvr)
	return nil
}

// findRulesFor maps a target resource event to reconcile requests for all
// PatchRules whose target kind and conditions match.
func (r *PatchRuleReconciler) findRulesFor(gvk schema.GroupVersionKind) handler.TypedMapFunc[*unstructured.Unstructured, reconcile.Request] {
	return func(ctx context.Context, target *unstructured.Unstructured) []reconcile.Request {
		var list v1.PatchRuleList
		if err := r.List(ctx, &list); err != nil {
			log.FromContext(ctx).Error(err, "list PatchRules")
			return nil
		}

		var reqs []reconcile.Request
		for _, rule := range list.Items {
			if schema.FromAPIVersionAndKind(rule.Spec.Target.APIVersion, rule.Spec.Target.Kind) != gvk {
				continue
			}
			if !matches(target.Object, rule.Spec.Target.Conditions.Data) {
				continue
			}
			reqs = append(reqs, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: rule.Name, Namespace: rule.Namespace},
			})
		}
		return reqs
	}
}
