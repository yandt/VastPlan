package authenticationv1

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"
)

func validateMethodMessage(message any) error {
	switch typed := message.(type) {
	case *DescribeResult:
		seen := map[string]struct{}{}
		for _, method := range typed.Methods {
			if _, duplicate := seen[method.MethodID]; duplicate {
				return fmt.Errorf("Authentication Method 重复: %s", method.MethodID)
			}
			seen[method.MethodID] = struct{}{}
			if err := validateMethodDescriptor(method); err != nil {
				return err
			}
		}
	case *BeginResult:
		return validateMethodResult(typed.Result)
	case *ContinueRequest:
		seen := map[string]struct{}{}
		for _, response := range typed.Responses {
			if _, duplicate := seen[response.FieldID]; duplicate {
				return fmt.Errorf("Authentication 字段响应重复: %s", response.FieldID)
			}
			seen[response.FieldID] = struct{}{}
		}
	case *ContinueResult:
		return validateMethodResult(typed.Result)
	case *ResendResult:
		return validateMethodResult(typed.Result)
	}
	return nil
}

func validateMethodDescriptor(method MethodDescriptor) error {
	if err := validateLocalizedText(method.DisplayName); err != nil {
		return err
	}
	switch method.Kind {
	case MethodPassword:
		if method.Interaction != InteractionForm || method.SupportsResend || !contains(method.AMR, "pwd") {
			return errors.New("password Method 必须是不可 resend 的 form 且声明 pwd AMR")
		}
	case MethodOneTimeCode:
		if method.Interaction != InteractionForm || !contains(method.AMR, "otp") {
			return errors.New("one-time-code Method 必须是 form 且声明 otp AMR")
		}
	case MethodRedirect:
		if method.Interaction != InteractionRedirect || method.SupportsResend {
			return errors.New("redirect Method 的交互类型无效")
		}
	case MethodPasskey:
		if method.Interaction != InteractionNative || method.SupportsResend {
			return errors.New("passkey Method 的交互类型无效")
		}
	}
	return nil
}

func validateMethodResult(result MethodResult) error {
	switch result.State {
	case StateChallenge:
		if result.Step == nil || result.Evidence != nil || result.ReasonCode != "" {
			return errors.New("challenge 只能携带 step")
		}
		return validateStep(*result.Step)
	case StateAuthenticated:
		if result.Evidence == nil || result.Step != nil || result.ReasonCode != "" {
			return errors.New("authenticated 只能携带 evidence")
		}
		return validateEvidence(*result.Evidence)
	case StateRejected, StateLocked, StateExpired:
		if result.Step != nil || result.Evidence != nil || !knownReasonCode(result.ReasonCode) {
			return errors.New("拒绝、锁定或过期结果只能携带通用 reasonCode")
		}
	case StateCancelled:
		if result.Step != nil || result.Evidence != nil {
			return errors.New("cancelled 不得携带 step 或 evidence")
		}
	}
	return nil
}

func knownReasonCode(code string) bool {
	switch code {
	case ReasonInvalidCredentials, ReasonChallengeRejected, ReasonChallengeExpired,
		ReasonRateLimited, ReasonMethodUnavailable, ReasonTransactionInvalid:
		return true
	default:
		return false
	}
}

func validateStep(step AuthenticationStep) error {
	for _, text := range []LocalizedText{step.Title, step.Description, step.SubmitLabel} {
		if err := validateLocalizedText(text); err != nil {
			return err
		}
	}
	if step.ExpiresAt.IsZero() {
		return errors.New("Authentication Step 缺少 expiresAt")
	}
	if step.ResendAfter != nil && step.ResendAfter.After(step.ExpiresAt) {
		return errors.New("resendAfter 不得晚于 step expiresAt")
	}
	if step.Kind == StepRedirect {
		if len(step.Fields) != 0 || step.RedirectURI == "" {
			return errors.New("redirect Step 只能携带 redirectUri")
		}
		parsed, err := url.Parse(step.RedirectURI)
		if err != nil || parsed.Host == "" || (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.User != nil {
			return errors.New("redirectUri 必须是无凭据 HTTP(S) URL")
		}
		return nil
	}
	if step.RedirectURI != "" || len(step.Fields) == 0 {
		return errors.New("非 redirect Step 必须只携带字段")
	}
	seen := map[string]struct{}{}
	for _, field := range step.Fields {
		if _, duplicate := seen[field.ID]; duplicate {
			return fmt.Errorf("Authentication Step 字段重复: %s", field.ID)
		}
		seen[field.ID] = struct{}{}
		if err := validateField(field); err != nil {
			return fmt.Errorf("Authentication 字段 %s: %w", field.ID, err)
		}
	}
	requiredKind := map[StepKind]FieldKind{StepIdentifier: FieldIdentifier, StepPassword: FieldPassword, StepOneTimeCode: FieldOneTimeCode, StepContextSelection: FieldSelect}[step.Kind]
	for _, field := range step.Fields {
		if field.Kind == requiredKind {
			return nil
		}
	}
	return fmt.Errorf("Authentication Step %s 缺少对应字段", step.Kind)
}

func validateField(field AuthenticationField) error {
	if err := validateLocalizedText(field.Label); err != nil {
		return err
	}
	if err := validateLocalizedText(field.Help); err != nil {
		return err
	}
	for _, choice := range field.Choices {
		if err := validateLocalizedText(choice.Label); err != nil {
			return err
		}
	}
	if field.MinLength > field.MaxLength {
		return errors.New("minLength 不能大于 maxLength")
	}
	switch field.Kind {
	case FieldIdentifier:
		if !contains([]string{"username", "email", "tel"}, field.Autocomplete) || len(field.Choices) != 0 || field.MaxLength > 320 {
			return errors.New("identifier 字段语义无效")
		}
	case FieldPassword:
		if field.Autocomplete != "current-password" || len(field.Choices) != 0 || field.MaxLength > 1024 {
			return errors.New("password 字段必须使用 current-password 且不携带 choices")
		}
	case FieldOneTimeCode:
		if field.Autocomplete != "one-time-code" || len(field.Choices) != 0 || field.MinLength < 4 || field.MaxLength > 32 {
			return errors.New("one-time-code 字段长度或 autocomplete 无效")
		}
	case FieldSelect:
		if field.Autocomplete != "off" || len(field.Choices) == 0 {
			return errors.New("select 字段必须携带 choices 且关闭 autocomplete")
		}
	}
	return nil
}

func validateEvidence(evidence AuthenticationEvidence) error {
	if evidence.AuthenticatedAt.IsZero() || !evidence.ExpiresAt.After(evidence.AuthenticatedAt) || evidence.ExpiresAt.Sub(evidence.AuthenticatedAt) > time.Minute {
		return errors.New("Authentication Evidence 有效期必须在 (0, 1m] 内")
	}
	if !contains(evidence.AMR, stringForMethod(evidence.MethodID)) && (evidence.MethodID == "password" || evidence.MethodID == "one-time-code") {
		return errors.New("Authentication Evidence AMR 与标准 Method 不匹配")
	}
	return nil
}

func stringForMethod(method string) string {
	if method == "password" {
		return "pwd"
	}
	if method == "one-time-code" {
		return "otp"
	}
	return method
}

func contains(values []string, expected string) bool {
	for _, value := range values {
		if strings.EqualFold(value, expected) {
			return true
		}
	}
	return false
}
