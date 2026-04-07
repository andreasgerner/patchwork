package patcher

// BuildRevertPatch builds a JSON merge patch that reverts all previously applied
// additions and restores all previously removed entries.
func BuildRevertPatch(appliedAdditions, priorValues, removedEntries map[string]interface{}) map[string]interface{} {
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

// BuildDiffRevertPatch builds a patch to revert additions/removals that were in
// oldState but not in newState (entries removed from the PatchRule spec).
func BuildDiffRevertPatch(
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
