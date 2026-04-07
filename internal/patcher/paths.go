package patcher

import patchworkv1 "github.com/andreasgerner/patchwork/api/v1"

// CollectAdditionPaths walks an additions tree and returns all leaf key paths
// as dot-separated strings (e.g. "metadata.annotations.foo").
func CollectAdditionPaths(data map[string]interface{}, prefix string) []string {
	var paths []string
	for key, val := range data {
		path := key
		if prefix != "" {
			path = prefix + "." + key
		}
		switch child := val.(type) {
		case map[string]interface{}:
			paths = append(paths, CollectAdditionPaths(child, path)...)
		default:
			paths = append(paths, path)
		}
	}
	return paths
}

// CollectRemovalPaths walks a removals tree and returns all leaf key paths.
// Removal leaves are string arrays listing keys to remove under a parent.
func CollectRemovalPaths(data map[string]interface{}, prefix string) []string {
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
			paths = append(paths, CollectRemovalPaths(child, path)...)
		}
	}
	return paths
}

// CollectTrackedPaths extracts all leaf paths from a TargetState's applied
// additions and removed entries (for conflict comparison with other rules).
func CollectTrackedPaths(state patchworkv1.TargetState) []string {
	var paths []string
	paths = append(paths, CollectAdditionPaths(state.AppliedAdditions.Data, "")...)
	// RemovedEntries stores original values in a nested map (not arrays),
	// so we walk it like additions to extract paths.
	paths = append(paths, CollectAdditionPaths(state.RemovedEntries.Data, "")...)
	return paths
}
