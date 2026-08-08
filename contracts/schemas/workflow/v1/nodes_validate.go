package workflowv1

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

var (
	artifactNodePath = regexp.MustCompile(`^workflow-nodes/(?:[A-Za-z0-9_-]+/)*[A-Za-z0-9_.-]+\.json$`)
	errorCode        = regexp.MustCompile(`^[a-z][a-z0-9]*(?:[._-][a-z0-9]+)+$`)
)

func ValidateNodeTemplate(descriptor NodeTemplateDescriptor) error {
	if !validNodeDescriptorIdentity(descriptor.ID, descriptor.Contract, descriptor.Title) || descriptor.CompilerContract != NodeTemplateProtocol {
		return errors.New("workflow node template identity is invalid")
	}
	if err := validateArtifactDocument(descriptor.ConfigurationSchema); err != nil {
		return fmt.Errorf("workflow node template configuration schema: %w", err)
	}
	if err := validateArtifactDocument(descriptor.Expansion); err != nil {
		return fmt.Errorf("workflow node template expansion: %w", err)
	}
	return validateOutcomes(descriptor.Outcomes)
}

func ValidateNodeProvider(descriptor NodeProviderDescriptor) error {
	if !validNodeDescriptorIdentity(descriptor.ID, descriptor.Contract, descriptor.Title) || descriptor.EffectContract != NodeEffectProtocol {
		return errors.New("workflow node provider identity is invalid")
	}
	if err := validateArtifactDocument(descriptor.ConfigurationSchema); err != nil {
		return fmt.Errorf("workflow node provider configuration schema: %w", err)
	}
	for name, document := range map[string]*ArtifactDocumentRef{"input": descriptor.InputSchema, "output": descriptor.OutputSchema} {
		if document != nil {
			if err := validateArtifactDocument(*document); err != nil {
				return fmt.Errorf("workflow node provider %s schema: %w", name, err)
			}
		}
	}
	if err := validateOperation(OperationDescriptor{Capability: descriptor.Capability, Operation: descriptor.Operation, Permission: descriptor.Permission}); err != nil {
		return err
	}
	return validateOutcomes(descriptor.Outcomes)
}

func ValidateNodeEffect(effect NodeEffect, allowedOutcomes []string) error {
	allowed := map[string]struct{}{}
	for _, outcome := range allowedOutcomes {
		allowed[outcome] = struct{}{}
	}
	switch effect.Kind {
	case NodeEffectComplete:
		if _, ok := allowed[effect.Outcome]; !ok || effect.Wait != nil || effect.RetryAt != nil || effect.ErrorCode != "" || effect.Reason != "" {
			return errors.New("workflow complete effect is invalid")
		}
	case NodeEffectWait:
		if effect.Wait == nil || effect.Outcome != "" || effect.RetryAt != nil || effect.ErrorCode != "" || len(effect.Facts) != 0 || !qualifiedID.MatchString(effect.Wait.EventContract) || !bounded(effect.Wait.CorrelationID, 256) || effect.Wait.Deadline.IsZero() {
			return errors.New("workflow wait effect is invalid")
		}
	case NodeEffectRetryAfter:
		if effect.RetryAt == nil || effect.RetryAt.IsZero() || !bounded(effect.Reason, 512) || effect.Outcome != "" || effect.Wait != nil || effect.ErrorCode != "" || len(effect.Facts) != 0 {
			return errors.New("workflow retry effect is invalid")
		}
	case NodeEffectFail:
		if !errorCode.MatchString(effect.ErrorCode) || !bounded(effect.Reason, 512) || effect.Outcome != "" || effect.Wait != nil || effect.RetryAt != nil || len(effect.Facts) != 0 {
			return errors.New("workflow fail effect is invalid")
		}
	default:
		return errors.New("workflow node effect kind is invalid")
	}
	return nil
}

func validNodeDescriptorIdentity(id, contract, title string) bool {
	return len(id) <= 160 && qualifiedID.MatchString(id) && semver.MatchString(contract) && bounded(title, 160)
}

func validateArtifactDocument(document ArtifactDocumentRef) error {
	if !artifactNodePath.MatchString(document.Path) || strings.Contains(document.Path, "..") || len(document.SHA256) != 64 {
		return errors.New("artifact document reference is invalid")
	}
	for _, value := range document.SHA256 {
		if value < '0' || value > '9' && value < 'a' || value > 'f' {
			return errors.New("artifact document digest is invalid")
		}
	}
	return nil
}

func validateOutcomes(outcomes []string) error {
	if len(outcomes) == 0 || len(outcomes) > 32 {
		return errors.New("workflow node must declare 1..32 outcomes")
	}
	seen := map[string]struct{}{}
	for _, outcome := range outcomes {
		if len(outcome) > 96 || !localID.MatchString(outcome) {
			return fmt.Errorf("workflow node outcome %q is invalid", outcome)
		}
		if _, duplicate := seen[outcome]; duplicate {
			return fmt.Errorf("workflow node outcome %q is duplicated", outcome)
		}
		seen[outcome] = struct{}{}
	}
	return nil
}

func bounded(value string, maximum int) bool {
	trimmed := strings.TrimSpace(value)
	return trimmed != "" && len(trimmed) <= maximum
}
