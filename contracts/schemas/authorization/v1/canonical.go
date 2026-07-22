package authorizationv1

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
)

// NormalizeAuthorizationIR returns a deep copy with all set-like collections
// sorted and all instants normalized to UTC. Input slices are never mutated.
func NormalizeAuthorizationIR(policy AuthorizationIR) (AuthorizationIR, error) {
	raw, err := json.Marshal(policy)
	if err != nil {
		return AuthorizationIR{}, err
	}
	var normalized AuthorizationIR
	if err := json.Unmarshal(raw, &normalized); err != nil {
		return AuthorizationIR{}, err
	}
	for profileIndex := range normalized.ProviderProfiles {
		profile := &normalized.ProviderProfiles[profileIndex]
		sort.Slice(profile.Exchange, func(i, j int) bool {
			if profile.Exchange[i].ProviderID != profile.Exchange[j].ProviderID {
				return profile.Exchange[i].ProviderID < profile.Exchange[j].ProviderID
			}
			return profile.Exchange[i].PluginID < profile.Exchange[j].PluginID
		})
	}
	sort.Slice(normalized.ProviderProfiles, func(i, j int) bool { return normalized.ProviderProfiles[i].ID < normalized.ProviderProfiles[j].ID })
	for domainIndex := range normalized.Domains {
		sort.Strings(normalized.Domains[domainIndex].Delegation.Permissions)
	}
	sort.Slice(normalized.Domains, func(i, j int) bool { return normalized.Domains[i].ID < normalized.Domains[j].ID })
	for roleIndex := range normalized.Roles {
		role := &normalized.Roles[roleIndex]
		for statementIndex := range role.Statements {
			statement := &role.Statements[statementIndex]
			sort.Strings(statement.Permissions)
			if statement.Resource != nil {
				sort.Strings(statement.Resource.IDs)
				for key := range statement.Resource.Labels {
					sort.Strings(statement.Resource.Labels[key])
				}
			}
			for constraintIndex := range statement.Constraints {
				sort.Strings(statement.Constraints[constraintIndex].Values)
			}
			sort.Slice(statement.Constraints, func(i, j int) bool {
				left, right := statement.Constraints[i], statement.Constraints[j]
				if left.Source != right.Source {
					return left.Source < right.Source
				}
				if left.Key != right.Key {
					return left.Key < right.Key
				}
				return left.Operator < right.Operator
			})
		}
		sort.Slice(role.Statements, func(i, j int) bool { return role.Statements[i].ID < role.Statements[j].ID })
	}
	sort.Slice(normalized.Roles, func(i, j int) bool {
		if normalized.Roles[i].DomainID != normalized.Roles[j].DomainID {
			return normalized.Roles[i].DomainID < normalized.Roles[j].DomainID
		}
		if normalized.Roles[i].ID != normalized.Roles[j].ID {
			return normalized.Roles[i].ID < normalized.Roles[j].ID
		}
		return normalized.Roles[i].Revision < normalized.Roles[j].Revision
	})
	for index := range normalized.Bindings {
		normalized.Bindings[index].NotBefore = normalized.Bindings[index].NotBefore.UTC()
		normalized.Bindings[index].ExpiresAt = normalized.Bindings[index].ExpiresAt.UTC()
	}
	sort.Slice(normalized.Bindings, func(i, j int) bool {
		if normalized.Bindings[i].ID != normalized.Bindings[j].ID {
			return normalized.Bindings[i].ID < normalized.Bindings[j].ID
		}
		return normalized.Bindings[i].Revision < normalized.Bindings[j].Revision
	})
	for index := range normalized.Revocations {
		normalized.Revocations[index].EffectiveAt = normalized.Revocations[index].EffectiveAt.UTC()
	}
	sort.Slice(normalized.Revocations, func(i, j int) bool {
		if normalized.Revocations[i].Revision != normalized.Revocations[j].Revision {
			return normalized.Revocations[i].Revision < normalized.Revocations[j].Revision
		}
		return normalized.Revocations[i].ID < normalized.Revocations[j].ID
	})
	return normalized, nil
}

func CanonicalAuthorizationIR(policy AuthorizationIR) ([]byte, error) {
	normalized, err := NormalizeAuthorizationIR(policy)
	if err != nil {
		return nil, err
	}
	var output bytes.Buffer
	encoder := json.NewEncoder(&output)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(normalized); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(output.Bytes(), []byte("\n")), nil
}

func AuthorizationIRDigest(policy AuthorizationIR) (string, error) {
	raw, err := CanonicalAuthorizationIR(policy)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:]), nil
}

func DigestRawDocument(raw json.RawMessage) (string, error) {
	var compact bytes.Buffer
	if err := json.Compact(&compact, raw); err != nil {
		return "", err
	}
	digest := sha256.Sum256(compact.Bytes())
	return hex.EncodeToString(digest[:]), nil
}
