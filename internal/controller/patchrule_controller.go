// Package controller implements the PatchRule reconciler.
package controller

import (
	"context"
	"fmt"
	"sync"

	patchworkv1 "github.com/andreasgerner/patchwork/api/v1"
	"github.com/andreasgerner/patchwork/internal/patcher"

	"k8s.io/apimachinery/pkg/api/errors"
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

	// MaxConcurrentReconciles is the number of concurrent reconcile loops.
	// Defaults to 1 if unset.
	MaxConcurrentReconciles int

	dynCtrl     controller.Controller
	cache       cache.Cache
	watchedGVRs map[schema.GroupVersionResource]bool
	mu          sync.Mutex
}

// +kubebuilder:rbac:groups=patchwork.io,resources=patchrules,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=patchwork.io,resources=patchrules/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=patchwork.io,resources=patchrules/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch
//
// SECURITY NOTE: The wildcard RBAC below grants the controller get/list/watch/patch
// on ALL resources in the cluster. This is required because PatchRules can target
// any resource type dynamically. For restricted environments, replace the wildcard
// ClusterRole with one that lists only the specific resource types you intend to
// patch (e.g. Ingress, Deployment, ConfigMap).
// +kubebuilder:rbac:groups="*",resources="*",verbs=get;list;watch;patch

// targetGVKIndexField is the field index key used to look up PatchRules by their
// target GroupVersionKind. This avoids listing every PatchRule in the cluster
// for conflict checks and sibling enqueue.
const targetGVKIndexField = ".spec.target.gvk"

// targetGVKValue returns the canonical string key for a PatchRule's target GVK,
// used both when indexing and when querying the index.
func targetGVKValue(apiVersion, kind string) string {
	return apiVersion + "/" + kind
}

// SetupWithManager registers the controller with the manager.
func (r *PatchRuleReconciler) SetupWithManager(mgr ctrl.Manager) error {
	r.cache = mgr.GetCache()
	r.watchedGVRs = make(map[schema.GroupVersionResource]bool)

	// Index PatchRules by target GVK so we can efficiently scope list calls.
	if err := mgr.GetFieldIndexer().IndexField(
		context.Background(),
		&patchworkv1.PatchRule{},
		targetGVKIndexField,
		func(obj client.Object) []string {
			rule, ok := obj.(*patchworkv1.PatchRule)
			if !ok {
				return nil
			}
			return []string{targetGVKValue(rule.Spec.Target.APIVersion, rule.Spec.Target.Kind)}
		},
	); err != nil {
		return fmt.Errorf("index %s: %w", targetGVKIndexField, err)
	}

	c, err := ctrl.NewControllerManagedBy(mgr).
		For(&patchworkv1.PatchRule{}).
		// Re-enqueue conflicted siblings when any PatchRule changes spec (generation-gated).
		Watches(&patchworkv1.PatchRule{},
			handler.EnqueueRequestsFromMapFunc(r.enqueueSiblingRules),
			builder.WithPredicates(predicate.GenerationChangedPredicate{}),
		).
		WithOptions(controller.Options{
			MaxConcurrentReconciles: max(1, r.MaxConcurrentReconciles),
		}).
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
	logger := log.FromContext(ctx)

	var rule patchworkv1.PatchRule
	if err := r.Get(ctx, req.NamespacedName, &rule); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Snapshot for deferred status patch
	statusBase := rule.DeepCopy()

	// Always patch status at the end of reconcile.
	// On conflict (resource version mismatch), request an immediate requeue.
	defer func() {
		if err := r.Status().Patch(ctx, &rule, client.MergeFrom(statusBase)); err != nil {
			if errors.IsConflict(err) {
				logger.V(1).Info("status patch conflict, will requeue")
				retRes = ctrl.Result{Requeue: true}
				return
			}
			retErr = kerrors.NewAggregate([]error{retErr, err})
		}
	}()

	// Handle deletion
	if !rule.DeletionTimestamp.IsZero() {
		if controllerutil.ContainsFinalizer(&rule, patchworkv1.PatchRuleFinalizer) {
			logger.Info("reverting all patches for deleted PatchRule")
			result, err := r.reconcileDelete(ctx, &rule)
			if err != nil {
				return result, err
			}
			controllerutil.RemoveFinalizer(&rule, patchworkv1.PatchRuleFinalizer)
			if err := r.Patch(ctx, &rule, client.MergeFrom(statusBase)); err != nil {
				return ctrl.Result{}, err
			}
		}
		return ctrl.Result{}, nil
	}

	// Ensure finalizer (via Patch, not Update)
	if !controllerutil.ContainsFinalizer(&rule, patchworkv1.PatchRuleFinalizer) {
		original := rule.DeepCopy()
		controllerutil.AddFinalizer(&rule, patchworkv1.PatchRuleFinalizer)
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
	rule *patchworkv1.PatchRule,
	matchedTargetKeys []string,
) (conflictingRule string, overlappingPaths []string, err error) {
	if len(matchedTargetKeys) == 0 {
		return "", nil, nil
	}

	ourPaths := make(map[string]bool)
	for _, p := range patcher.CollectAdditionPaths(rule.Spec.Additions.Data, "") {
		ourPaths[p] = true
	}
	for _, p := range patcher.CollectRemovalPaths(rule.Spec.Removals.Data, "") {
		ourPaths[p] = true
	}
	if len(ourPaths) == 0 {
		return "", nil, nil
	}

	matchedSet := make(map[string]bool, len(matchedTargetKeys))
	for _, k := range matchedTargetKeys {
		matchedSet[k] = true
	}

	var list patchworkv1.PatchRuleList
	if err := r.List(ctx, &list,
		client.MatchingFields{targetGVKIndexField: targetGVKValue(rule.Spec.Target.APIVersion, rule.Spec.Target.Kind)},
	); err != nil {
		return "", nil, err
	}

	for i := range list.Items {
		other := &list.Items[i]

		if other.UID == rule.UID {
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
			for _, p := range patcher.CollectTrackedPaths(otherState) {
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
func hasPriority(a, b *patchworkv1.PatchRule) bool {
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
func getPriority(rule *patchworkv1.PatchRule) int32 {
	if rule.Spec.Priority != nil {
		return *rule.Spec.Priority
	}
	return 1
}

// enqueueSiblingRules maps a PatchRule event to reconcile requests for all
// other PatchRules targeting the same GVK that are currently conflicted.
func (r *PatchRuleReconciler) enqueueSiblingRules(ctx context.Context, obj client.Object) []reconcile.Request {
	rule, ok := obj.(*patchworkv1.PatchRule)
	if !ok {
		return nil
	}

	var list patchworkv1.PatchRuleList
	if err := r.List(ctx, &list,
		client.MatchingFields{targetGVKIndexField: targetGVKValue(rule.Spec.Target.APIVersion, rule.Spec.Target.Kind)},
	); err != nil {
		log.FromContext(ctx).Error(err, "list PatchRules for sibling enqueue")
		return nil
	}

	var reqs []reconcile.Request
	for _, other := range list.Items {
		if other.UID == rule.UID {
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
//
// This intentionally does NOT filter by namespace: a PatchRule in namespace A
// can declare targets in any namespace. The actual target scoping happens in
// reconcileNormal, which lists targets the PatchRule can see via its RBAC.
// The index ensures we only scan PatchRules targeting the same GVK.
func (r *PatchRuleReconciler) findRulesFor(gvk schema.GroupVersionKind) handler.TypedMapFunc[*unstructured.Unstructured, reconcile.Request] {
	gvkKey := targetGVKValue(gvk.GroupVersion().String(), gvk.Kind)

	return func(ctx context.Context, target *unstructured.Unstructured) []reconcile.Request {
		var list patchworkv1.PatchRuleList
		if err := r.List(ctx, &list,
			client.MatchingFields{targetGVKIndexField: gvkKey},
		); err != nil {
			log.FromContext(ctx).Error(err, "list PatchRules")
			return nil
		}

		var reqs []reconcile.Request
		for _, rule := range list.Items {
			if !patcher.Matches(target.Object, rule.Spec.Target.Conditions.Data) {
				continue
			}
			reqs = append(reqs, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: rule.Name, Namespace: rule.Namespace},
			})
		}
		return reqs
	}
}
