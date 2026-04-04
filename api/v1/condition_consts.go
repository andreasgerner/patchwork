package v1

// Condition types for PatchRule status.
const (
	// ConditionReady indicates the PatchRule is actively patching targets.
	ConditionReady = "Ready"

	// ConditionConflicted indicates the PatchRule overlaps with a higher-priority
	// rule on the same key paths for the same target resources.
	ConditionConflicted = "Conflicted"
)

// Condition reasons for PatchRule status.
const (
	ReasonReady            = "Ready"
	ReasonNotReady         = "NotReady"
	ReasonConflict         = "PathConflict"
	ReasonConflictResolved = "ConflictResolved"
)

// PatchRuleFinalizer is the finalizer added to every PatchRule to ensure
// applied patches are reverted before the CR is deleted.
const PatchRuleFinalizer = "patchrule.patchwork.io"

// FieldManager is the server-side field manager name used when applying patches.
const FieldManager = "patchwork"
