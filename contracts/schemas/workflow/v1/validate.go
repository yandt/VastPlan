package workflowv1

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

var (
	qualifiedID = regexp.MustCompile(`^[a-z][a-z0-9]*(?:[._-][a-z0-9]+)+$`)
	localID     = regexp.MustCompile(`^[a-z][a-z0-9]*(?:[._-][a-z0-9]+)*$`)
	operationID = regexp.MustCompile(`^[a-z][A-Za-z0-9]*$`)
	semver      = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+$`)
)

func ValidateFeature(feature FeatureDescriptor) error {
	if len(feature.ID) > 160 || len(feature.ResourceKind) > 160 || !qualifiedID.MatchString(feature.ID) || !semver.MatchString(feature.Contract) || !qualifiedID.MatchString(feature.ResourceKind) {
		return errors.New("workflow feature identity or contract is invalid")
	}
	if len(feature.Actions) == 0 || len(feature.Actions) > 64 {
		return errors.New("workflow feature must declare 1..64 actions")
	}
	if feature.Prepare != nil {
		if err := validateOperation(*feature.Prepare); err != nil {
			return fmt.Errorf("workflow prepare operation is invalid: %w", err)
		}
	}
	seen := map[string]ActionDescriptor{}
	for _, action := range feature.Actions {
		if len(action.ID) > 160 || len(action.Capability) > 160 || len(action.Operation) > 96 || len(action.Permission) > 160 || !qualifiedID.MatchString(action.ID) || !qualifiedID.MatchString(action.Capability) || !operationID.MatchString(action.Operation) || !qualifiedID.MatchString(action.Permission) {
			return fmt.Errorf("workflow action %q is invalid", action.ID)
		}
		if _, exists := seen[action.ID]; exists {
			return fmt.Errorf("workflow action %q is duplicated", action.ID)
		}
		seen[action.ID] = action
	}
	if feature.UnboundPolicy == "" {
		feature.UnboundPolicy = UnboundDeny
	}
	switch feature.UnboundPolicy {
	case UnboundDeny:
		if feature.UnboundActionID != "" {
			return errors.New("deny unbound policy cannot declare a direct action")
		}
	case UnboundDirect:
		action, exists := seen[feature.UnboundActionID]
		if !exists || !action.Terminal {
			return errors.New("direct unbound policy must reference a terminal action")
		}
	default:
		return errors.New("workflow feature unbound policy is invalid")
	}
	return nil
}

func validateOperation(operation OperationDescriptor) error {
	if len(operation.Capability) > 160 || len(operation.Operation) > 96 || len(operation.Permission) > 160 || !qualifiedID.MatchString(operation.Capability) || !operationID.MatchString(operation.Operation) || !qualifiedID.MatchString(operation.Permission) {
		return errors.New("operation target is invalid")
	}
	return nil
}

func ValidateDefinition(definition Definition, feature FeatureDescriptor) error {
	if len(definition.ID) > 160 || len(definition.EntryNodeID) > 96 || !qualifiedID.MatchString(definition.ID) || definition.Revision < 1 || definition.FeatureID != feature.ID || !localID.MatchString(definition.EntryNodeID) {
		return errors.New("workflow definition identity is invalid")
	}
	if len(definition.Nodes) == 0 || len(definition.Nodes) > 128 {
		return errors.New("workflow definition must contain 1..128 nodes")
	}
	actions := map[string]struct{}{}
	for _, action := range feature.Actions {
		actions[action.ID] = struct{}{}
	}
	nodes := make(map[string]Node, len(definition.Nodes))
	for _, node := range definition.Nodes {
		if len(node.ID) > 96 || !localID.MatchString(node.ID) {
			return fmt.Errorf("workflow node id %q is invalid", node.ID)
		}
		if !qualifiedID.MatchString(string(node.Type.ID)) || node.Type.Contract != CoreNodeContract {
			return fmt.Errorf("workflow node %q has unsupported type %q@%s", node.ID, node.Type.ID, node.Type.Contract)
		}
		if _, exists := nodes[node.ID]; exists {
			return fmt.Errorf("workflow node %q is duplicated", node.ID)
		}
		if err := validateNode(node, actions); err != nil {
			return err
		}
		nodes[node.ID] = node
	}
	if _, exists := nodes[definition.EntryNodeID]; !exists {
		return errors.New("workflow entry node does not exist")
	}
	visiting, visited := map[string]bool{}, map[string]bool{}
	var visit func(string) error
	visit = func(id string) error {
		if visiting[id] {
			return fmt.Errorf("workflow cycle at node %q is not supported", id)
		}
		if visited[id] {
			return nil
		}
		node, exists := nodes[id]
		if !exists {
			return fmt.Errorf("workflow edge references missing node %q", id)
		}
		visiting[id] = true
		for _, next := range nextNodes(node) {
			if err := visit(next); err != nil {
				return err
			}
		}
		visiting[id], visited[id] = false, true
		return nil
	}
	if err := visit(definition.EntryNodeID); err != nil {
		return err
	}
	if len(visited) != len(nodes) {
		return errors.New("workflow definition contains unreachable nodes")
	}
	return nil
}

func ValidatePreparedResource(resource PreparedResource, feature FeatureDescriptor) error {
	if resource.Resource.Kind != feature.ResourceKind || !boundedWorkflowText(resource.Resource.ID, 256) || resource.Revision < 1 {
		return errors.New("prepared workflow resource is invalid")
	}
	if feature.DigestRequired && !isHexDigest(resource.Digest) {
		return errors.New("prepared workflow resource digest is invalid")
	}
	if len(resource.Projection) > 256<<10 {
		return errors.New("prepared workflow resource projection is too large")
	}
	return nil
}

func boundedWorkflowText(value string, maximum int) bool {
	trimmed := strings.TrimSpace(value)
	return trimmed != "" && len(trimmed) <= maximum
}

func isHexDigest(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, character := range value {
		if character < '0' || character > '9' && character < 'a' || character > 'f' {
			return false
		}
	}
	return true
}

func validateNode(node Node, actions map[string]struct{}) error {
	if len(node.Config) != 0 || len(node.Transitions) != 0 {
		return fmt.Errorf("core workflow node %q cannot contain provider configuration", node.ID)
	}
	switch node.Type.ID {
	case NodeManual:
		if strings.TrimSpace(node.Title) == "" || len(node.Outcomes) == 0 || node.ActionID != "" || node.Next != "" || node.Result != "" {
			return fmt.Errorf("manual node %q is invalid", node.ID)
		}
		for outcome, next := range node.Outcomes {
			if !localID.MatchString(outcome) || !localID.MatchString(next) {
				return fmt.Errorf("manual node %q has invalid outcome", node.ID)
			}
		}
	case NodeAction:
		if _, ok := actions[node.ActionID]; !ok || !localID.MatchString(node.Next) || len(node.Outcomes) != 0 || node.Result != "" || len(node.Roles) != 0 {
			return fmt.Errorf("action node %q is invalid", node.ID)
		}
	case NodeEnd:
		if node.Result != ResultSucceeded && node.Result != ResultRejected && node.Result != ResultCancelled {
			return fmt.Errorf("end node %q has invalid result", node.ID)
		}
		if node.Next != "" || node.ActionID != "" || len(node.Outcomes) != 0 || len(node.Roles) != 0 {
			return fmt.Errorf("end node %q is invalid", node.ID)
		}
	default:
		return fmt.Errorf("workflow node %q has unsupported type %q", node.ID, node.Type.ID)
	}
	return nil
}

func nextNodes(node Node) []string {
	if node.Type.ID == NodeAction {
		return []string{node.Next}
	}
	if node.Type.ID != NodeManual {
		return nil
	}
	result := make([]string, 0, len(node.Outcomes))
	for _, next := range node.Outcomes {
		result = append(result, next)
	}
	sort.Strings(result)
	return result
}

func DefinitionDigest(definition Definition) (string, error) {
	raw, err := json.Marshal(definition)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}
