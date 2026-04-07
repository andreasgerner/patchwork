package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	patchworkv1 "github.com/andreasgerner/patchwork/api/v1"
	"github.com/andreasgerner/patchwork/internal/patcher"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// matchedTarget pairs a target key with its unstructured object, collected
// in a single pass over the target list.
type matchedTarget struct {
	key    string
	target *unstructured.Unstructured
}

// reconcileDelete reverts all tracked patches and returns. Called when the
// PatchRule has a non-zero DeletionTimestamp.
func (r *PatchRuleReconciler) reconcileDelete(ctx context.Context, rule *patchworkv1.PatchRule) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	if len(rule.Status.Targets) == 0 {
		return ctrl.Result{}, nil
	}

	gvk := schema.FromAPIVersionAndKind(rule.Spec.Target.APIVersion, rule.Spec.Target.Kind)

	for key, state := range rule.Status.Targets {
		if err := r.applyRevertPatch(ctx, gvk, key, state); err != nil {
			r.Recorder.Eventf(rule, corev1.EventTypeWarning, "RevertFailed",
				"failed to revert target %s: %v", key, err)
			return ctrl.Result{}, err
		}
		logger.V(1).Info("reverted", "target", key)
	}
	return ctrl.Result{}, nil
}

// reconcileNormal is the main reconcile path for active (non-deleting) PatchRules.
// It checks conflicts, patches matching targets, reverts stale state, and updates status.
func (r *PatchRuleReconciler) reconcileNormal(ctx context.Context, rule *patchworkv1.PatchRule) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	// Resolve GVK
	gvk := schema.FromAPIVersionAndKind(rule.Spec.Target.APIVersion, rule.Spec.Target.Kind)
	mapping, err := r.RESTMapper().RESTMapping(gvk.GroupKind(), gvk.Version)
	if err != nil {
		logger.Error(err, "cannot resolve target",
			"apiVersion", rule.Spec.Target.APIVersion, "kind", rule.Spec.Target.Kind)
		setNotReadyCondition(rule, fmt.Sprintf("cannot resolve target %s/%s: %v",
			rule.Spec.Target.APIVersion, rule.Spec.Target.Kind, err))
		return ctrl.Result{}, err
	}

	if err := r.ensureWatch(mapping.Resource, gvk); err != nil {
		setNotReadyCondition(rule, fmt.Sprintf("watch setup failed: %v", err))
		return ctrl.Result{}, err
	}

	// List and filter targets in a single pass
	var targetList unstructured.UnstructuredList
	targetList.SetGroupVersionKind(gvk.GroupVersion().WithKind(gvk.Kind + "List"))
	if err := r.List(ctx, &targetList); err != nil {
		setNotReadyCondition(rule, fmt.Sprintf("list targets failed: %v", err))
		return ctrl.Result{}, err
	}

	var matched []matchedTarget
	for i := range targetList.Items {
		target := &targetList.Items[i]
		if patcher.Matches(target.Object, rule.Spec.Target.Conditions.Data) {
			matched = append(matched, matchedTarget{
				key:    targetKey(target.GetNamespace(), target.GetName()),
				target: target,
			})
		}
	}

	// Check for conflicts with higher-priority rules
	matchedKeys := make([]string, len(matched))
	for i, m := range matched {
		matchedKeys[i] = m.key
	}

	conflictRule, conflictPaths, err := r.checkConflicts(ctx, rule, matchedKeys)
	if err != nil {
		setNotReadyCondition(rule, fmt.Sprintf("conflict check failed: %v", err))
		return ctrl.Result{}, err
	}
	if conflictRule != "" {
		msg := fmt.Sprintf("conflicts with %s on paths: %s",
			conflictRule, strings.Join(conflictPaths, ", "))
		logger.Info("conflict detected", "conflictsWith", conflictRule, "paths", conflictPaths)
		setConflictedCondition(rule, msg)
		r.Recorder.Eventf(rule, corev1.EventTypeWarning, "Conflicted",
			"rejected: %s has higher priority on paths: %s", conflictRule, strings.Join(conflictPaths, ", "))
		return ctrl.Result{}, nil
	}

	// Clear conflict if previously set
	if isConflicted(rule) {
		clearConflictedCondition(rule)
		r.Recorder.Event(rule, corev1.EventTypeNormal, "ConflictResolved",
			"conflict resolved, rule is now active")
	}

	// Patch each matching target
	oldTargets := rule.Status.Targets
	newTargets := make(map[string]patchworkv1.TargetState, len(matched))

	for _, m := range matched {
		state, err := r.reconcilePatch(ctx, rule, m, oldTargets)
		if err != nil {
			setNotReadyCondition(rule, fmt.Sprintf("patch failed for %s: %v", m.key, err))
			return ctrl.Result{}, err
		}
		newTargets[m.key] = state
	}

	// Revert targets that are no longer matched
	if err := r.revertStaleTargets(ctx, gvk, oldTargets, newTargets); err != nil {
		setNotReadyCondition(rule, fmt.Sprintf("revert stale targets failed: %v", err))
		return ctrl.Result{}, err
	}

	// Revert spec entries that were removed from still-matched targets
	if err := r.revertDroppedSpecEntries(ctx, gvk, oldTargets, newTargets); err != nil {
		setNotReadyCondition(rule, fmt.Sprintf("revert dropped spec entries failed: %v", err))
		return ctrl.Result{}, err
	}

	// Update status fields
	rule.Status.ObservedGeneration = rule.Generation
	rule.Status.TargetCount = int32(len(matched))
	rule.Status.Targets = newTargets
	setReadyCondition(rule)

	if len(matched) > 0 {
		r.Recorder.Eventf(rule, corev1.EventTypeNormal, "Patched",
			"applied to %d target(s) with priority %d", len(matched), getPriority(rule))
	}

	return ctrl.Result{}, nil
}

// reconcilePatch handles a single target: captures state, builds and applies
// the patch, and returns the new TargetState.
func (r *PatchRuleReconciler) reconcilePatch(
	ctx context.Context,
	rule *patchworkv1.PatchRule,
	m matchedTarget,
	oldTargets map[string]patchworkv1.TargetState,
) (patchworkv1.TargetState, error) {
	logger := log.FromContext(ctx)

	// Capture original values BEFORE patching (target.Object is the pre-patch snapshot)
	priorValues := patcher.CaptureOverwrittenValues(
		m.target.Object, rule.Spec.Additions.Data, rule.Spec.Overwrite)
	removedEntries := patcher.CaptureRemovedEntries(
		m.target.Object, rule.Spec.Removals.Data)

	patch := patcher.BuildPatch(m.target, rule)
	if patch != nil {
		patchBytes, err := json.Marshal(patch)
		if err != nil {
			return patchworkv1.TargetState{}, fmt.Errorf("marshal patch: %w", err)
		}
		if err := r.Patch(ctx, m.target,
			client.RawPatch(types.MergePatchType, patchBytes),
			client.FieldOwner(patchworkv1.FieldManager)); err != nil {
			logger.Error(err, "patch failed",
				"namespace", m.target.GetNamespace(), "name", m.target.GetName())
			return patchworkv1.TargetState{}, err
		}
		logger.V(1).Info("patched",
			"namespace", m.target.GetNamespace(), "name", m.target.GetName())
	}

	// NOTE: captureAppliedAdditions intentionally uses m.target.Object AFTER the
	// patch call above. The Patch response updates the in-memory object, so we
	// see the post-patch state. This is correct because we're recording which
	// *desired* additions are now present on the target (for revert tracking),
	// not what the original values were (that was captured in priorValues above).
	appliedAdditions := patcher.CaptureAppliedAdditions(
		m.target.Object, rule.Spec.Additions.Data, rule.Spec.Overwrite)

	// Merge with old prior values to preserve true originals across reconciles
	if oldState, ok := oldTargets[m.key]; ok {
		priorValues = patcher.MergePriorValues(oldState.PriorValues.Data, priorValues)
		removedEntries = patcher.MergePriorValues(oldState.RemovedEntries.Data, removedEntries)
	}

	return patchworkv1.TargetState{
		AppliedAdditions: patchworkv1.NestedObject{Data: appliedAdditions},
		PriorValues:      patchworkv1.NestedObject{Data: priorValues},
		RemovedEntries:   patchworkv1.NestedObject{Data: removedEntries},
	}, nil
}

// revertStaleTargets reverts patches on targets that were previously tracked
// but are no longer in the new matched set.
func (r *PatchRuleReconciler) revertStaleTargets(
	ctx context.Context,
	gvk schema.GroupVersionKind,
	oldTargets, newTargets map[string]patchworkv1.TargetState,
) error {
	for key, oldState := range oldTargets {
		if _, stillMatched := newTargets[key]; stillMatched {
			continue
		}
		if err := r.applyRevertPatch(ctx, gvk, key, oldState); err != nil {
			return err
		}
		log.FromContext(ctx).V(1).Info("reverted stale target", "target", key)
	}
	return nil
}

// revertDroppedSpecEntries reverts additions/removals that were removed from
// the PatchRule spec but whose targets are still matched.
func (r *PatchRuleReconciler) revertDroppedSpecEntries(
	ctx context.Context,
	gvk schema.GroupVersionKind,
	oldTargets, newTargets map[string]patchworkv1.TargetState,
) error {
	for key, oldState := range oldTargets {
		newState, ok := newTargets[key]
		if !ok {
			continue // handled by revertStaleTargets
		}

		diffPatch := patcher.BuildDiffRevertPatch(
			oldState.AppliedAdditions.Data, newState.AppliedAdditions.Data,
			oldState.PriorValues.Data,
			oldState.RemovedEntries.Data, newState.RemovedEntries.Data,
		)
		if diffPatch == nil {
			continue
		}

		namespace, name := parseTargetKey(key)
		target := &unstructured.Unstructured{}
		target.SetGroupVersionKind(gvk)
		if err := r.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, target); err != nil {
			if client.IgnoreNotFound(err) == nil {
				continue
			}
			return err
		}

		patchBytes, err := json.Marshal(diffPatch)
		if err != nil {
			return fmt.Errorf("marshal diff revert patch: %w", err)
		}

		if err := r.Patch(ctx, target,
			client.RawPatch(types.MergePatchType, patchBytes),
			client.FieldOwner(patchworkv1.FieldManager)); err != nil {
			return fmt.Errorf("revert dropped spec entries for %s: %w", key, err)
		}

		log.FromContext(ctx).V(1).Info("reverted dropped spec entries", "target", key)
	}
	return nil
}

// applyRevertPatch fetches a target and applies a full revert patch. Silently
// skips targets that no longer exist.
func (r *PatchRuleReconciler) applyRevertPatch(
	ctx context.Context,
	gvk schema.GroupVersionKind,
	key string,
	state patchworkv1.TargetState,
) error {
	revertPatch := patcher.BuildRevertPatch(
		state.AppliedAdditions.Data,
		state.PriorValues.Data,
		state.RemovedEntries.Data,
	)
	if revertPatch == nil {
		return nil
	}

	namespace, name := parseTargetKey(key)
	target := &unstructured.Unstructured{}
	target.SetGroupVersionKind(gvk)
	if err := r.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, target); err != nil {
		if client.IgnoreNotFound(err) == nil {
			return nil // target already gone
		}
		return err
	}

	patchBytes, err := json.Marshal(revertPatch)
	if err != nil {
		return fmt.Errorf("marshal revert patch for %s: %w", key, err)
	}

	if err := r.Patch(ctx, target,
		client.RawPatch(types.MergePatchType, patchBytes),
		client.FieldOwner(patchworkv1.FieldManager)); err != nil {
		return fmt.Errorf("revert patch for %s: %w", key, err)
	}
	return nil
}

// targetKey builds a map key from namespace and name.
func targetKey(namespace, name string) string {
	if namespace == "" {
		return name
	}
	return namespace + "/" + name
}

// parseTargetKey splits a map key back into namespace and name.
func parseTargetKey(key string) (namespace, name string) {
	parts := strings.SplitN(key, "/", 2)
	if len(parts) == 2 {
		return parts[0], parts[1]
	}
	return "", parts[0]
}
