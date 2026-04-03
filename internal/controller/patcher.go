package controller

import (
	v1 "patchwork/api/v1"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

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

// buildPatch produces a JSON merge patch for the target based on the PatchRule
// spec. Returns nil if conditions don't match or nothing needs changing.
func buildPatch(target *unstructured.Unstructured, rule *v1.PatchRule) map[string]interface{} {
	if !matches(target.Object, rule.Spec.Target.Conditions.Data) {
		return nil
	}

	patch := map[string]interface{}{}

	setDefaults(target.Object, rule.Spec.Additions.Data, patch, rule.Spec.Overwrite)
	applyDeletes(target.Object, rule.Spec.Removals.Data, patch)

	if len(patch) == 0 {
		return nil
	}
	return patch
}

// setDefaults walks the defaults tree and writes entries into patch for values
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

// applyDeletes walks the deletes tree. At leaf nodes (string arrays), it sets
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
				name, ok := item.(string)
				if ok {
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
