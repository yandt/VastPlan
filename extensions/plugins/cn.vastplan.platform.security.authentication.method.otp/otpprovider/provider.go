package otpprovider

import (
	"context"
	"crypto/hmac"
	"strings"
	"sync"
	"time"

	authenticationv1 "cdsoft.com.cn/VastPlan/contracts/schemas/authentication/v1"
	contractv1 "cdsoft.com.cn/VastPlan/core/shared/go/contract/v1"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
)

type Provider struct {
	mu         sync.RWMutex
	profiles   map[string]Profile
	store      ChallengeStore
	hmacKey    []byte
	now        func() time.Time
	stateFile  string
	controller controllerState
}

func New(configuration Configuration, stores ...ChallengeStore) (*Provider, error) {
	normalized, err := configuration.normalized()
	if err != nil {
		return nil, err
	}
	key, err := randomKey()
	if err != nil {
		return nil, err
	}
	store := ChallengeStore(NewMemoryChallengeStore(normalized.Capacity))
	if len(stores) > 0 && stores[0] != nil {
		store = stores[0]
	}
	provider := &Provider{profiles: normalized.Profiles, store: store, hmacKey: key, now: func() time.Time { return time.Now().UTC() }}
	if err := provider.configureController(normalized); err != nil {
		return nil, err
	}
	return provider, nil
}

func (p *Provider) profile(id string) (Profile, bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	profile, ok := p.profiles[id]
	return profile, ok
}

func (p *Provider) begin(request authenticationv1.BeginRequest) authenticationv1.BeginResult {
	profile, ok := p.profile(request.ProviderProfileID)
	if !ok || profile.MethodID != request.MethodID {
		return authenticationv1.BeginResult{Result: terminal(authenticationv1.StateRejected, authenticationv1.ReasonMethodUnavailable)}
	}
	stepID, err := randomID()
	if err != nil {
		return authenticationv1.BeginResult{Result: terminal(authenticationv1.StateRejected, authenticationv1.ReasonMethodUnavailable)}
	}
	expires := p.now().Add(profile.ttl())
	value := challenge{Phase: phaseIdentifier, ProfileID: request.ProviderProfileID, Profile: profile, MethodID: request.MethodID, StepID: stepID, Locale: request.Locale, ExpiresAt: expires}
	if err := p.store.Create(request.TransactionID, value); err != nil {
		return authenticationv1.BeginResult{Result: terminal(authenticationv1.StateRejected, authenticationv1.ReasonRateLimited)}
	}
	return authenticationv1.BeginResult{Result: authenticationv1.MethodResult{State: authenticationv1.StateChallenge, Step: identifierStep(stepID, profile.Channel, expires)}}
}

func (p *Provider) continueAuthentication(ctx context.Context, host sdk.Host, call *contractv1.CallContext, request authenticationv1.ContinueRequest) authenticationv1.ContinueResult {
	current, ok := p.store.Load(request.TransactionID)
	if !ok || current.StepID != request.StepID || !p.now().Before(current.ExpiresAt) {
		return authenticationv1.ContinueResult{Result: terminal(authenticationv1.StateExpired, authenticationv1.ReasonTransactionInvalid)}
	}
	switch current.Phase {
	case phaseIdentifier:
		identifier, ok := exactResponse(request.Responses, "identifier")
		if !ok {
			return authenticationv1.ContinueResult{Result: terminal(authenticationv1.StateRejected, authenticationv1.ReasonChallengeRejected)}
		}
		return authenticationv1.ContinueResult{Result: p.startCodeChallenge(ctx, host, call, request.TransactionID, current, identifier, false)}
	case phaseCode:
		code, ok := exactResponse(request.Responses, "code")
		if !ok {
			return authenticationv1.ContinueResult{Result: terminal(authenticationv1.StateRejected, authenticationv1.ReasonChallengeRejected)}
		}
		return authenticationv1.ContinueResult{Result: p.verifyCode(request.TransactionID, current, code)}
	default:
		return authenticationv1.ContinueResult{Result: terminal(authenticationv1.StateRejected, authenticationv1.ReasonTransactionInvalid)}
	}
}

func (p *Provider) startCodeChallenge(ctx context.Context, host sdk.Host, call *contractv1.CallContext, transactionID string, current challenge, identifier string, resend bool) authenticationv1.MethodResult {
	profile := current.Profile
	code, err := randomCode(profile.CodeLength)
	if err != nil {
		p.store.Delete(transactionID)
		return terminal(authenticationv1.StateRejected, authenticationv1.ReasonMethodUnavailable)
	}
	stepID, err := randomID()
	if err != nil {
		p.store.Delete(transactionID)
		return terminal(authenticationv1.StateRejected, authenticationv1.ReasonMethodUnavailable)
	}
	now := p.now()
	expires := now.Add(profile.ttl())
	normalized := strings.TrimSpace(identifier)
	reserved := current
	reserved.Phase = phaseDelivering
	reserved.StepID = stepID
	reserved.Identifier = normalized
	reserved.SubjectID = ""
	reserved.CodeMAC = p.codeMAC(transactionID, code)
	reserved.ExpiresAt = expires
	reserved.ResendAt = now.Add(profile.resendDelay())
	reserved.Attempts = 0
	if resend {
		reserved.Resends++
	}
	if !p.store.CompareAndSwap(transactionID, current.Revision, reserved) {
		return terminal(authenticationv1.StateRejected, authenticationv1.ReasonTransactionInvalid)
	}
	delivery, deliveryErr := deliver(ctx, host, call, authenticationv1.DeliveryRequest{ChallengeID: "challenge." + stepID, DeliveryProfileID: profile.DeliveryProfileID, Channel: profile.Channel, Identifier: normalized, Locale: current.Locale, Code: code, ExpiresAt: expires})
	latest, ok := p.store.Load(transactionID)
	if !ok || latest.Phase != phaseDelivering || latest.StepID != stepID {
		return terminal(authenticationv1.StateRejected, authenticationv1.ReasonTransactionInvalid)
	}
	if deliveryErr != nil {
		p.store.Delete(transactionID)
		return terminal(authenticationv1.StateRejected, authenticationv1.ReasonMethodUnavailable)
	}
	latest.Phase = phaseCode
	if delivery.Accepted {
		latest.SubjectID = delivery.SubjectID
	}
	if !p.store.CompareAndSwap(transactionID, latest.Revision, latest) {
		return terminal(authenticationv1.StateRejected, authenticationv1.ReasonTransactionInvalid)
	}
	return authenticationv1.MethodResult{State: authenticationv1.StateChallenge, Step: codeStep(stepID, expires, latest.ResendAt, profile.CodeLength)}
}

func (p *Provider) verifyCode(transactionID string, current challenge, code string) authenticationv1.MethodResult {
	profile := current.Profile
	valid := len(code) == profile.CodeLength && hmac.Equal(current.CodeMAC, p.codeMAC(transactionID, code)) && current.SubjectID != ""
	if valid {
		evidenceID, evidenceErr := randomID()
		nonce, nonceErr := randomID()
		if evidenceErr != nil || nonceErr != nil {
			return terminal(authenticationv1.StateRejected, authenticationv1.ReasonMethodUnavailable)
		}
		if !p.store.Consume(transactionID, current.Revision) {
			return terminal(authenticationv1.StateRejected, authenticationv1.ReasonTransactionInvalid)
		}
		now := p.now()
		return authenticationv1.MethodResult{State: authenticationv1.StateAuthenticated, Evidence: &authenticationv1.AuthenticationEvidence{EvidenceID: "otp." + evidenceID, TransactionID: transactionID, MethodID: current.MethodID, ProviderID: ProviderID, Subject: authenticationv1.SubjectIdentity{ID: current.SubjectID, Issuer: profile.Issuer}, AMR: []string{"otp"}, ACR: "aal1", AuthenticatedAt: now, ExpiresAt: now.Add(30 * time.Second), Nonce: nonce}}
	}
	current.Attempts++
	if current.Attempts >= profile.MaxAttempts {
		p.store.Consume(transactionID, current.Revision)
		return terminal(authenticationv1.StateLocked, authenticationv1.ReasonRateLimited)
	}
	if !p.store.CompareAndSwap(transactionID, current.Revision, current) {
		return terminal(authenticationv1.StateRejected, authenticationv1.ReasonTransactionInvalid)
	}
	return authenticationv1.MethodResult{State: authenticationv1.StateChallenge, Step: codeStep(current.StepID, current.ExpiresAt, current.ResendAt, profile.CodeLength)}
}

func (p *Provider) resend(ctx context.Context, host sdk.Host, call *contractv1.CallContext, request authenticationv1.ResendRequest) authenticationv1.ResendResult {
	current, ok := p.store.Load(request.TransactionID)
	if !ok || current.Phase != phaseCode || current.StepID != request.StepID || !p.now().Before(current.ExpiresAt) {
		return authenticationv1.ResendResult{Result: terminal(authenticationv1.StateExpired, authenticationv1.ReasonTransactionInvalid)}
	}
	profile := current.Profile
	if p.now().Before(current.ResendAt) {
		return authenticationv1.ResendResult{Result: authenticationv1.MethodResult{State: authenticationv1.StateChallenge, Step: codeStep(current.StepID, current.ExpiresAt, current.ResendAt, profile.CodeLength)}}
	}
	if current.Resends >= profile.maxResends {
		p.store.Consume(request.TransactionID, current.Revision)
		return authenticationv1.ResendResult{Result: terminal(authenticationv1.StateLocked, authenticationv1.ReasonRateLimited)}
	}
	return authenticationv1.ResendResult{Result: p.startCodeChallenge(ctx, host, call, request.TransactionID, current, current.Identifier, true)}
}
