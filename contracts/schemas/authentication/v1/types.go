// Package authenticationv1 defines the language-neutral login method,
// assertion, and pre-session Access Profile contracts. It contains no password
// database, delivery SDK, browser cookie, or policy-engine objects.
package authenticationv1

import "time"

const (
	SchemaVersion = "v1"
	Protocol      = "authentication.method.v1"
)

type LocalizedText map[string]string

type SubjectIdentity struct {
	ID     string `json:"id"`
	Issuer string `json:"issuer"`
}

type MethodKind string

const (
	MethodPassword    MethodKind = "password"
	MethodOneTimeCode MethodKind = "one-time-code"
	MethodRedirect    MethodKind = "redirect"
	MethodPasskey     MethodKind = "passkey"
)

type InteractionKind string

const (
	InteractionForm     InteractionKind = "form"
	InteractionRedirect InteractionKind = "redirect"
	InteractionNative   InteractionKind = "native"
)

type MethodDescriptor struct {
	MethodID       string          `json:"methodId"`
	ProviderID     string          `json:"providerId"`
	Kind           MethodKind      `json:"kind"`
	Interaction    InteractionKind `json:"interaction"`
	DisplayName    LocalizedText   `json:"displayName"`
	AMR            []string        `json:"amr"`
	ACR            string          `json:"acr"`
	SupportsResend bool            `json:"supportsResend"`
}

type FieldKind string

const (
	FieldIdentifier  FieldKind = "identifier"
	FieldPassword    FieldKind = "password"
	FieldOneTimeCode FieldKind = "one-time-code"
	FieldSelect      FieldKind = "select"
)

type FieldChoice struct {
	Value string        `json:"value"`
	Label LocalizedText `json:"label"`
}

// AuthenticationField is a bounded semantic prompt. Method Providers cannot
// return arbitrary JSON Schema, scripts, HTML, or framework components.
type AuthenticationField struct {
	ID           string        `json:"id"`
	Kind         FieldKind     `json:"kind"`
	Label        LocalizedText `json:"label"`
	Help         LocalizedText `json:"help"`
	Autocomplete string        `json:"autocomplete"`
	Required     bool          `json:"required"`
	MinLength    int           `json:"minLength"`
	MaxLength    int           `json:"maxLength"`
	Choices      []FieldChoice `json:"choices"`
}

type StepKind string

const (
	StepIdentifier       StepKind = "identifier"
	StepPassword         StepKind = "password"
	StepOneTimeCode      StepKind = "one-time-code"
	StepRedirect         StepKind = "redirect"
	StepContextSelection StepKind = "context-selection"
)

type AuthenticationStep struct {
	StepID      string                `json:"stepId"`
	Kind        StepKind              `json:"kind"`
	Title       LocalizedText         `json:"title"`
	Description LocalizedText         `json:"description"`
	SubmitLabel LocalizedText         `json:"submitLabel"`
	Fields      []AuthenticationField `json:"fields"`
	RedirectURI string                `json:"redirectUri,omitempty"`
	ExpiresAt   time.Time             `json:"expiresAt"`
	ResendAfter *time.Time            `json:"resendAfter,omitempty"`
}

type FieldResponse struct {
	FieldID string `json:"fieldId"`
	Value   string `json:"value"`
}

type MethodState string

const (
	StateChallenge     MethodState = "challenge"
	StateAuthenticated MethodState = "authenticated"
	StateRejected      MethodState = "rejected"
	StateLocked        MethodState = "locked"
	StateExpired       MethodState = "expired"
	StateCancelled     MethodState = "cancelled"
)

const (
	ReasonInvalidCredentials = "authentication.invalid_credentials"
	ReasonChallengeRejected  = "authentication.challenge_rejected"
	ReasonChallengeExpired   = "authentication.challenge_expired"
	ReasonRateLimited        = "authentication.rate_limited"
	ReasonMethodUnavailable  = "authentication.method_unavailable"
	ReasonTransactionInvalid = "authentication.transaction_invalid"
)

type AuthenticationEvidence struct {
	EvidenceID      string          `json:"evidenceId"`
	TransactionID   string          `json:"transactionId"`
	MethodID        string          `json:"methodId"`
	ProviderID      string          `json:"providerId"`
	Subject         SubjectIdentity `json:"subject"`
	AMR             []string        `json:"amr"`
	ACR             string          `json:"acr"`
	AuthenticatedAt time.Time       `json:"authenticatedAt"`
	ExpiresAt       time.Time       `json:"expiresAt"`
	Nonce           string          `json:"nonce"`
}

type MethodResult struct {
	State      MethodState             `json:"state"`
	Step       *AuthenticationStep     `json:"step,omitempty"`
	Evidence   *AuthenticationEvidence `json:"evidence,omitempty"`
	ReasonCode string                  `json:"reasonCode,omitempty"`
}
