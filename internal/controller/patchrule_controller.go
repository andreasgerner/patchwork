package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	v1 "patchwork/api/v1"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

const fieldManager = "patchwork"

// PatchRuleReconciler watches PatchRule CRs and patches matching target resources.
type PatchRuleReconciler struct {
	client.Client

	ctrl        controller.Controller
	cache       cache.Cache
	watchedGVRs map[schema.GroupVersionResource]bool
	mu          sync.Mutex
}

// SetupWithManager registers the controller with the manager.
func (r *PatchRuleReconciler) SetupWithManager(mgr ctrl.Manager) error {
	r.cache = mgr.GetCache()
	r.watchedGVRs = make(map[schema.GroupVersionResource]bool)

	c, err := ctrl.NewControllerManagedBy(mgr).
		For(&v1.PatchRule{}).
		Build(r)
	if err != nil {
		return err
	}
	r.ctrl = c
	return nil
}

// +kubebuilder:rbac:groups=patchwork.io,resources=patchrules,verbs=get;list;watch
// +kubebuilder:rbac:groups=patchwork.io,resources=patchrules/status,verbs=get;update;patch
// +kubebuilder:rbac:groups="*",resources="*",verbs=get;list;watch;patch

// Reconcile ensures all matching target resources have the configured patches applied.
func (r *PatchRuleReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := log.FromContext(ctx)

	// 1. Fetch the PatchRule
	var rule v1.PatchRule
	if err := r.Get(ctx, req.NamespacedName, &rule); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// 2. Resolve target kind to a concrete API resource
	gvk := schema.FromAPIVersionAndKind(rule.Spec.Target.APIVersion, rule.Spec.Target.Kind)
	mapping, err := r.RESTMapper().RESTMapping(gvk.GroupKind(), gvk.Version)
	if err != nil {
		log.Error(err, "cannot resolve target",
			"apiVersion", rule.Spec.Target.APIVersion, "kind", rule.Spec.Target.Kind)
		return ctrl.Result{}, err
	}

	// 3. Start watching this resource type if we aren't already
	if err := r.ensureWatch(mapping.Resource, gvk); err != nil {
		return ctrl.Result{}, err
	}

	// 4. List all instances of the target kind
	var targets unstructured.UnstructuredList
	targets.SetGroupVersionKind(gvk.GroupVersion().WithKind(gvk.Kind + "List"))
	if err := r.List(ctx, &targets); err != nil {
		return ctrl.Result{}, err
	}

	// 5. Patch each matching target
	var matched int32
	for i := range targets.Items {
		target := &targets.Items[i]

		if !matches(target.Object, rule.Spec.Target.Conditions.Data) {
			continue
		}
		matched++

		patch := buildPatch(target, &rule)
		if patch == nil {
			continue
		}

		patchBytes, err := json.Marshal(patch)
		if err != nil {
			return ctrl.Result{}, fmt.Errorf("marshal patch: %w", err)
		}

		if err := r.Patch(ctx, target, client.RawPatch(types.MergePatchType, patchBytes), client.FieldOwner(fieldManager)); err != nil {
			log.Error(err, "patch failed",
				"namespace", target.GetNamespace(), "name", target.GetName())
			return ctrl.Result{}, err
		}

		log.V(1).Info("patched",
			"namespace", target.GetNamespace(), "name", target.GetName())
	}

	// 6. Patch status
	base := rule.DeepCopy()
	rule.Status.ObservedGeneration = rule.Generation
	rule.Status.TargetCount = matched
	rule.Status.Active = true
	if err := r.Status().Patch(ctx, &rule, client.MergeFrom(base)); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
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

	if err := r.ctrl.Watch(source.Kind(r.cache, obj, mapFn)); err != nil {
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
