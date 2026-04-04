package controller

import (
	v1 "github.com/andreasgerner/patchwork/api/v1"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// setReadyCondition marks the PatchRule as ready with a target count.
func setReadyCondition(rule *v1.PatchRule) {
	meta.SetStatusCondition(&rule.Status.Conditions, metav1.Condition{
		Type:               v1.ConditionReady,
		Status:             metav1.ConditionTrue,
		Reason:             v1.ReasonReady,
		ObservedGeneration: rule.Generation,
	})
}

// setNotReadyCondition marks the PatchRule as not ready with a message.
func setNotReadyCondition(rule *v1.PatchRule, message string) {
	meta.SetStatusCondition(&rule.Status.Conditions, metav1.Condition{
		Type:               v1.ConditionReady,
		Status:             metav1.ConditionFalse,
		Reason:             v1.ReasonNotReady,
		Message:            message,
		ObservedGeneration: rule.Generation,
	})
}

// setConflictedCondition marks the PatchRule as conflicted.
func setConflictedCondition(rule *v1.PatchRule, message string) {
	meta.SetStatusCondition(&rule.Status.Conditions, metav1.Condition{
		Type:               v1.ConditionConflicted,
		Status:             metav1.ConditionTrue,
		Reason:             v1.ReasonConflict,
		Message:            message,
		ObservedGeneration: rule.Generation,
	})
	// A conflicted rule is not ready
	setNotReadyCondition(rule, message)
}

// clearConflictedCondition marks the conflict as resolved.
func clearConflictedCondition(rule *v1.PatchRule) {
	meta.SetStatusCondition(&rule.Status.Conditions, metav1.Condition{
		Type:               v1.ConditionConflicted,
		Status:             metav1.ConditionFalse,
		Reason:             v1.ReasonConflictResolved,
		ObservedGeneration: rule.Generation,
	})
}

// isConflicted returns true if the PatchRule has an active Conflicted condition.
func isConflicted(rule *v1.PatchRule) bool {
	c := meta.FindStatusCondition(rule.Status.Conditions, v1.ConditionConflicted)
	return c != nil && c.Status == metav1.ConditionTrue
}
