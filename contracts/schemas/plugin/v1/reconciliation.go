package pluginv1

import (
	"errors"
	"fmt"
	"sort"
)

const (
	ReconcileActivate   = "activate"
	ReconcileReplace    = "replace"
	ReconcileDeactivate = "deactivate"
	ReconcileRetain     = "retain"
)

type ReconciliationTransition struct {
	Operation string
	Current   *PluginArtifactIdentity
	Candidate *PluginArtifactIdentity
	Index     ContributionIndexSnapshot
}

// ReconciliationAdapter owns target-specific rollout semantics. The generic
// planner never switches on a kernel name after this adapter is injected.
type ReconciliationAdapter interface {
	Target() string
	Transition(ReconciliationTransition) (string, error)
}

type ReconciliationAction struct {
	PluginID  string                  `json:"pluginId"`
	Operation string                  `json:"operation"`
	Strategy  string                  `json:"strategy"`
	Current   *PluginArtifactIdentity `json:"current,omitempty"`
	Candidate *PluginArtifactIdentity `json:"candidate,omitempty"`
}

type ReconciliationPlan struct {
	SchemaVersion      int                    `json:"schemaVersion"`
	Target             string                 `json:"target"`
	Generation         uint64                 `json:"generation"`
	SelectionDigest    string                 `json:"selectionDigest"`
	ContributionDigest string                 `json:"contributionDigest"`
	Actions            []ReconciliationAction `json:"actions"`
	Digest             string                 `json:"digest"`
}

func ValidateActivationSelection(selection ActivationSelection) error {
	if selection.SchemaVersion != PluginInventorySchemaVersion || selection.PolicyID == "" || !validPluginTarget(selection.Target) || selection.Generation == 0 || !isSHA256(selection.InventoryDigest) || !isSHA256(selection.ContributionDigest) || !isSHA256(selection.Digest) {
		return errors.New("Activation Selection 身份无效")
	}
	if _, err := uniqueArtifacts(selection.Artifacts); err != nil {
		return err
	}
	expected, err := activationSelectionDigest(selection)
	if err != nil || expected != selection.Digest {
		return errors.New("Activation Selection 摘要失配")
	}
	return nil
}

func ValidateReconciliationPlan(plan ReconciliationPlan) error {
	if plan.SchemaVersion != PluginInventorySchemaVersion || !validPluginTarget(plan.Target) || plan.Generation == 0 || !isSHA256(plan.SelectionDigest) || !isSHA256(plan.ContributionDigest) || !isSHA256(plan.Digest) {
		return errors.New("Reconciliation Plan 身份无效")
	}
	seen := map[string]struct{}{}
	for _, action := range plan.Actions {
		if action.PluginID == "" || action.Strategy == "" || (action.Operation != ReconcileActivate && action.Operation != ReconcileReplace && action.Operation != ReconcileDeactivate && action.Operation != ReconcileRetain) {
			return errors.New("Reconciliation Action 无效")
		}
		if _, duplicate := seen[action.PluginID]; duplicate {
			return fmt.Errorf("Reconciliation Action 插件重复: %s", action.PluginID)
		}
		seen[action.PluginID] = struct{}{}
	}
	expected, err := reconciliationPlanDigest(plan)
	if err != nil || expected != plan.Digest {
		return errors.New("Reconciliation Plan 摘要失配")
	}
	return nil
}

func PlanReconciliation(selection ActivationSelection, index ContributionIndexSnapshot, current []PluginArtifactIdentity, adapter ReconciliationAdapter) (ReconciliationPlan, error) {
	if adapter == nil || adapter.Target() != selection.Target || !validPluginTarget(selection.Target) {
		return ReconciliationPlan{}, errors.New("Reconciliation Adapter 与 Activation Selection 不匹配")
	}
	if err := ValidateContributionIndex(index); err != nil || index.Digest != selection.ContributionDigest || index.InventoryDigest != selection.InventoryDigest || index.Generation != selection.Generation {
		return ReconciliationPlan{}, errors.New("Reconciliation 输入快照不一致")
	}
	if err := ValidateActivationSelection(selection); err != nil {
		return ReconciliationPlan{}, err
	}
	desiredByID, err := uniqueArtifacts(selection.Artifacts)
	if err != nil {
		return ReconciliationPlan{}, err
	}
	currentByID, err := uniqueArtifacts(current)
	if err != nil {
		return ReconciliationPlan{}, err
	}
	ids := map[string]struct{}{}
	for id := range desiredByID {
		ids[id] = struct{}{}
	}
	for id := range currentByID {
		ids[id] = struct{}{}
	}
	ordered := make([]string, 0, len(ids))
	for id := range ids {
		ordered = append(ordered, id)
	}
	sort.Strings(ordered)
	actions := make([]ReconciliationAction, 0, len(ordered))
	for _, id := range ordered {
		currentValue, hasCurrent := currentByID[id]
		desiredValue, hasDesired := desiredByID[id]
		operation := ReconcileRetain
		var currentPtr, desiredPtr *PluginArtifactIdentity
		if hasCurrent {
			value := currentValue
			currentPtr = &value
		}
		if hasDesired {
			value := desiredValue
			desiredPtr = &value
		}
		switch {
		case !hasCurrent:
			operation = ReconcileActivate
		case !hasDesired:
			operation = ReconcileDeactivate
		case artifactIdentityKey(currentValue) != artifactIdentityKey(desiredValue):
			operation = ReconcileReplace
		}
		strategy, err := adapter.Transition(ReconciliationTransition{Operation: operation, Current: currentPtr, Candidate: desiredPtr, Index: index})
		if err != nil || strategy == "" {
			if err == nil {
				err = errors.New("Adapter 返回空策略")
			}
			return ReconciliationPlan{}, fmt.Errorf("规划插件 %s: %w", id, err)
		}
		actions = append(actions, ReconciliationAction{PluginID: id, Operation: operation, Strategy: strategy, Current: currentPtr, Candidate: desiredPtr})
	}
	plan := ReconciliationPlan{SchemaVersion: PluginInventorySchemaVersion, Target: selection.Target, Generation: selection.Generation, SelectionDigest: selection.Digest, ContributionDigest: index.Digest, Actions: actions}
	digest, err := reconciliationPlanDigest(plan)
	if err != nil {
		return ReconciliationPlan{}, err
	}
	plan.Digest = digest
	return plan, nil
}

func uniqueArtifacts(values []PluginArtifactIdentity) (map[string]PluginArtifactIdentity, error) {
	result := map[string]PluginArtifactIdentity{}
	for _, value := range values {
		if value.Ref.PluginID == "" || value.Ref.Version == "" || value.Ref.Channel == "" || !isSHA256(value.SHA256) {
			return nil, errors.New("Reconciliation 制品身份无效")
		}
		if _, duplicate := result[value.Ref.PluginID]; duplicate {
			return nil, fmt.Errorf("Reconciliation 插件身份重复: %s", value.Ref.PluginID)
		}
		result[value.Ref.PluginID] = value
	}
	return result, nil
}
