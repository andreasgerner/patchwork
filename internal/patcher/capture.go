// Package patcher provides pure functions for building, capturing, and reverting
// JSON merge patches on Kubernetes resources.
package patcher

// CaptureOverwrittenValues walks the additions tree and records the current
// target values for keys that will be overwritten.
func CaptureOverwrittenValues(target, additions map[string]interface{}, overwrite bool) map[string]interface{} {
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
			sub := CaptureOverwrittenValues(targetChild, val, overwrite)
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

// CaptureRemovedEntries walks the removals tree and records the current target
// values for keys that will be deleted.
func CaptureRemovedEntries(target, removals map[string]interface{}) map[string]interface{} {
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
			sub := CaptureRemovedEntries(targetChild, val)
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

// CaptureAppliedAdditions returns the subset of additions that were actually
// written to (or already match on) the target. Tracks what we own for revert.
func CaptureAppliedAdditions(target, additions map[string]interface{}, overwrite bool) map[string]interface{} {
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
			sub := CaptureAppliedAdditions(targetChild, val, overwrite)
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

// MergePriorValues merges old prior values with newly captured ones.
// Old values take precedence — they are the true originals.
func MergePriorValues(old, captured map[string]interface{}) map[string]interface{} {
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
				merged[k] = MergePriorValues(oldChild, capturedChild)
				continue
			}
		}
		merged[k] = v
	}
	return merged
}
