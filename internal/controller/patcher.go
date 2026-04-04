package controller

import (
	v1 "github.com/andreasgerner/patchwork/api/v1"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// collectAdditionPaths walks an additions tree and returns all leaf key paths
// as dot-separated strings (e.g. "metadata.annotations.foo").
func collectAdditionPaths(data map[string]interface{}, prefix string) []string {
	var paths []string
	for key, val := range data {
		path := key
		if prefix != "" {
			path = prefix + "." + key
		}
		switch child := val.(type) {
		case map[string]interface{}:
			paths = append(paths, collectAdditionPaths(child, path)...)
		default:
			paths = append(paths, path)
		}
	}
	return paths
}

// collectRemovalPaths walks a removals tree and returns all leaf key paths.
// Removal leaves are string arrays listing keys to remove under a parent.
func collectRemovalPaths(data map[string]interface{}, prefix string) []string {
	var paths []string
	for key, val := range data {
		path := key
		if prefix != "" {
			path = prefix + "." + key
		}
		switch child := val.(type) {
		case []interface{}:
			for _, item := range child {
				if name, ok := item.(string); ok {
					paths = append(paths, path+"."+name)
				}
			}
		case map[string]interface{}:
			paths = append(paths, collectRemovalPaths(child, path)...)
		}
	}
	return paths
}

// collectTrackedPaths extracts all leaf paths from a TargetState's applied
// additions and removed entries (for conflict comparison with other rules).
func collectTrackedPaths(state v1.TargetState) []string {
	var paths []string
	paths = append(paths, collectAdditionPaths(state.AppliedAdditions.Data, "")...)
	// RemovedEntries stores original values in a nested map (not arrays),
	// so we walk it like additions to extract paths.
	paths = append(paths, collectAdditionPaths(state.RemovedEntries.Data, "")...)
	return paths
}

// matches returns true if every leaf string in want exists with the same
// value in got. A nil/empty want always matches.
func matches(got, want map[string]interface{}) bool {
	for key, expected := range want {
		actual, ok := got[key]
		if !ok {
			return false
		}
		switch exp := expected.(type) {
		case string:
			if s, ok := actual.(string); !ok || s != exp {
				return false
			}
		case map[string]interface{}:
			child, ok := actual.(map[string]interface{})
			if !ok || !matches(child, exp) {
				return false
			}
		default:
			return false
		}
	}
	return true
}

// buildPatch produces a JSON merge patch for the target based on the PatchRule spec.
// The caller must pre-filter targets with matches(). Returns nil if nothing needs changing.
func buildPatch(target *unstructured.Unstructured, rule *v1.PatchRule) map[string]interface{} {
	patch := map[string]interface{}{}

	setDefaults(target.Object, rule.Spec.Additions.Data, patch, rule.Spec.Overwrite)
	applyDeletes(target.Object, rule.Spec.Removals.Data, patch)

	if len(patch) == 0 {
		return nil
	}
	return patch
}

// setDefaults walks the additions tree and writes entries into patch for values
// that need to be created or overwritten on the target.
func setDefaults(target, defaults, patch map[string]interface{}, overwrite bool) {
	for key, desired := range defaults {
		switch val := desired.(type) {
		case string:
			if cur, ok := target[key].(string); ok && cur == val {
				continue
			}
			if !overwrite {
				if _, exists := target[key]; exists {
					continue
				}
			}
			patch[key] = val

		case map[string]interface{}:
			sub := ensureChild(patch, key)
			targetChild, _ := target[key].(map[string]interface{})
			if targetChild == nil {
				targetChild = map[string]interface{}{}
			}
			setDefaults(targetChild, val, sub, overwrite)
			if len(sub) == 0 {
				delete(patch, key)
			}
		}
	}
}

// applyDeletes walks the removals tree. At leaf nodes (string arrays), it sets
// matching keys to null in the patch — JSON merge patch interprets null as deletion.
func applyDeletes(target, deletes, patch map[string]interface{}) {
	for key, value := range deletes {
		switch val := value.(type) {
		case []interface{}:
			targetChild, _ := target[key].(map[string]interface{})
			if targetChild == nil {
				continue
			}
			sub := ensureChild(patch, key)
			for _, item := range val {
				if name, ok := item.(string); ok {
					if _, exists := targetChild[name]; exists {
						sub[name] = nil
					}
				}
			}
			if len(sub) == 0 {
				delete(patch, key)
			}

		case map[string]interface{}:
			targetChild, _ := target[key].(map[string]interface{})
			if targetChild == nil {
				continue
			}
			sub := ensureChild(patch, key)
			applyDeletes(targetChild, val, sub)
			if len(sub) == 0 {
				delete(patch, key)
			}
		}
	}
}

// ensureChild returns patch[key] as a map, creating and attaching it if needed.
func ensureChild(patch map[string]interface{}, key string) map[string]interface{} {
	if sub, ok := patch[key].(map[string]interface{}); ok {
		return sub
	}
	sub := map[string]interface{}{}
	patch[key] = sub
	return sub
}

// captureOverwrittenValues walks the additions tree and records the current
// target values for keys that will be overwritten.
func captureOverwrittenValues(target, additions map[string]interface{}, overwrite bool) map[string]interface{} {
	prior := map[string]interface{}{}
	for key, desired := range additions {
		switch val := desired.(type) {
		case string:
			existing, exists := target[key]
			if !exists {
				continue
			}
			if cur, ok := existing.(string); ok && cur == val {
				continue
			}
			if !overwrite {
				continue
			}
			prior[key] = existing

		case map[string]interface{}:
			targetChild, _ := target[key].(map[string]interface{})
			if targetChild == nil {
				continue
			}
			sub := captureOverwrittenValues(targetChild, val, overwrite)
			if len(sub) > 0 {
				prior[key] = sub
			}
		}
	}
	if len(prior) == 0 {
		return nil
	}
	return prior
}

// captureRemovedEntries walks the removals tree and records the current target
// values for keys that will be deleted.
func captureRemovedEntries(target, removals map[string]interface{}) map[string]interface{} {
	captured := map[string]interface{}{}
	for key, value := range removals {
		switch val := value.(type) {
		case []interface{}:
			targetChild, _ := target[key].(map[string]interface{})
			if targetChild == nil {
				continue
			}
			sub := map[string]interface{}{}
			for _, item := range val {
				if name, ok := item.(string); ok {
					if existing, exists := targetChild[name]; exists {
						sub[name] = existing
					}
				}
			}
			if len(sub) > 0 {
				captured[key] = sub
			}

		case map[string]interface{}:
			targetChild, _ := target[key].(map[string]interface{})
			if targetChild == nil {
				continue
			}
			sub := captureRemovedEntries(targetChild, val)
			if len(sub) > 0 {
				captured[key] = sub
			}
		}
	}
	if len(captured) == 0 {
		return nil
	}
	return captured
}

// captureAppliedAdditions returns the subset of additions that were actually
// written to (or already match on) the target. Tracks what we own for revert.
func captureAppliedAdditions(target, additions map[string]interface{}, overwrite bool) map[string]interface{} {
	applied := map[string]interface{}{}
	for key, desired := range additions {
		switch val := desired.(type) {
		case string:
			if !overwrite {
				if existing, exists := target[key]; exists {
					if cur, _ := existing.(string); cur != val {
						continue
					}
				}
			}
			applied[key] = val
		case map[string]interface{}:
			targetChild, _ := target[key].(map[string]interface{})
			if targetChild == nil {
				targetChild = map[string]interface{}{}
			}
			sub := captureAppliedAdditions(targetChild, val, overwrite)
			if len(sub) > 0 {
				applied[key] = sub
			}
		}
	}
	if len(applied) == 0 {
		return nil
	}
	return applied
}

// buildRevertPatch builds a JSON merge patch that reverts all previously applied
// additions and restores all previously removed entries.
func buildRevertPatch(appliedAdditions, priorValues, removedEntries map[string]interface{}) map[string]interface{} {
	patch := map[string]interface{}{}
	revertAdditions(appliedAdditions, priorValues, patch)
	restoreRemovals(removedEntries, patch)
	if len(patch) == 0 {
		return nil
	}
	return patch
}

// revertAdditions walks applied additions. For each leaf: restore prior value
// if one exists, otherwise set to nil (delete via JSON merge patch).
func revertAdditions(applied, prior, patch map[string]interface{}) {
	for key, val := range applied {
		switch child := val.(type) {
		case string:
			if prior != nil {
				if orig, ok := prior[key]; ok {
					patch[key] = orig
					continue
				}
			}
			patch[key] = nil

		case map[string]interface{}:
			sub := ensureChild(patch, key)
			var priorChild map[string]interface{}
			if prior != nil {
				priorChild, _ = prior[key].(map[string]interface{})
			}
			revertAdditions(child, priorChild, sub)
			if len(sub) == 0 {
				delete(patch, key)
			}
		}
	}
}

// restoreRemovals walks removed entries and writes them back into the patch.
func restoreRemovals(removedEntries, patch map[string]interface{}) {
	for key, val := range removedEntries {
		switch child := val.(type) {
		case map[string]interface{}:
			sub := ensureChild(patch, key)
			restoreRemovals(child, sub)
			if len(sub) == 0 {
				delete(patch, key)
			}
		default:
			patch[key] = val
		}
	}
}

// buildDiffRevertPatch builds a patch to revert additions/removals that were in
// oldState but not in newState (entries removed from the PatchRule spec).
func buildDiffRevertPatch(
	oldAdditions, currentAdditions map[string]interface{},
	priorValues map[string]interface{},
	oldRemovedEntries, currentRemovedEntries map[string]interface{},
) map[string]interface{} {
	patch := map[string]interface{}{}
	revertDroppedAdditions(oldAdditions, currentAdditions, priorValues, patch)
	restoreDroppedRemovals(oldRemovedEntries, currentRemovedEntries, patch)
	if len(patch) == 0 {
		return nil
	}
	return patch
}

// revertDroppedAdditions finds keys in old additions absent from current and reverts them.
func revertDroppedAdditions(old, current, prior, patch map[string]interface{}) {
	for key, oldVal := range old {
		currentVal, exists := current[key]
		if !exists {
			switch oldChild := oldVal.(type) {
			case string:
				if prior != nil {
					if orig, ok := prior[key]; ok {
						patch[key] = orig
						continue
					}
				}
				patch[key] = nil
			case map[string]interface{}:
				sub := ensureChild(patch, key)
				var priorChild map[string]interface{}
				if prior != nil {
					priorChild, _ = prior[key].(map[string]interface{})
				}
				revertAdditions(oldChild, priorChild, sub)
				if len(sub) == 0 {
					delete(patch, key)
				}
			}
			continue
		}

		// Key exists in both — recurse into nested maps
		oldChild, oldIsMap := oldVal.(map[string]interface{})
		currentChild, currentIsMap := currentVal.(map[string]interface{})
		if oldIsMap && currentIsMap {
			sub := ensureChild(patch, key)
			var priorChild map[string]interface{}
			if prior != nil {
				priorChild, _ = prior[key].(map[string]interface{})
			}
			revertDroppedAdditions(oldChild, currentChild, priorChild, sub)
			if len(sub) == 0 {
				delete(patch, key)
			}
		}
	}
}

// restoreDroppedRemovals finds keys in old removed entries absent from current
// and restores them.
func restoreDroppedRemovals(old, current, patch map[string]interface{}) {
	for key, oldVal := range old {
		currentVal, exists := current[key]
		if !exists {
			switch child := oldVal.(type) {
			case map[string]interface{}:
				sub := ensureChild(patch, key)
				restoreRemovals(child, sub)
				if len(sub) == 0 {
					delete(patch, key)
				}
			default:
				patch[key] = oldVal
			}
			continue
		}

		oldChild, oldIsMap := oldVal.(map[string]interface{})
		currentChild, currentIsMap := currentVal.(map[string]interface{})
		if oldIsMap && currentIsMap {
			sub := ensureChild(patch, key)
			restoreDroppedRemovals(oldChild, currentChild, sub)
			if len(sub) == 0 {
				delete(patch, key)
			}
		}
	}
}

// mergePriorValues merges old prior values with newly captured ones.
// Old values take precedence — they are the true originals.
func mergePriorValues(old, captured map[string]interface{}) map[string]interface{} {
	if old == nil {
		return captured
	}
	if captured == nil {
		return old
	}
	merged := make(map[string]interface{}, len(old)+len(captured))
	for k, v := range captured {
		merged[k] = v
	}
	for k, v := range old {
		if oldChild, ok := v.(map[string]interface{}); ok {
			if capturedChild, ok := merged[k].(map[string]interface{}); ok {
				merged[k] = mergePriorValues(oldChild, capturedChild)
				continue
			}
		}
		merged[k] = v
	}
	return merged
}
