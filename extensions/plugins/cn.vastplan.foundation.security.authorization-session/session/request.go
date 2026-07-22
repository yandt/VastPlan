package session

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"regexp"
)

type ResolveRequest struct {
	ProviderProfileID string   `json:"providerProfileId"`
	Issuer            string   `json:"issuer"`
	Subject           string   `json:"subject"`
	TenantID          string   `json:"tenantId"`
	PortalID          string   `json:"portalId"`
	AMR               []string `json:"amr"`
	ACR               string   `json:"acr"`
}

type ResolveResult struct {
	SubjectID string    `json:"subjectId"`
	TenantID  string    `json:"tenantId"`
	Roles     []string  `json:"roles"`
	Policy    PolicyRef `json:"policy"`
	ExpiresAt string    `json:"expiresAt"`
}

type PolicyRef struct {
	ID       string `json:"id"`
	Revision uint64 `json:"revision"`
	Digest   string `json:"digest"`
}

var safeIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:@/-]{0,255}$`)

func parseResolveRequest(raw []byte) (ResolveRequest, error) {
	if len(raw) == 0 || len(raw) > 64<<10 {
		return ResolveRequest{}, errors.New("Authorization Session 请求大小无效")
	}
	var value ResolveRequest
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&value); err != nil {
		return ResolveRequest{}, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return ResolveRequest{}, errors.New("Authorization Session 请求只能包含一个 JSON 文档")
	}
	if !safeIDPattern.MatchString(value.ProviderProfileID) || !safeIDPattern.MatchString(value.Subject) || !safeIDPattern.MatchString(value.TenantID) || !safeIDPattern.MatchString(value.PortalID) || len(value.Issuer) < 1 || len(value.Issuer) > 512 || len(value.AMR) < 1 || len(value.AMR) > 16 || len(value.ACR) < 1 || len(value.ACR) > 128 {
		return ResolveRequest{}, errors.New("Authorization Session 请求字段无效")
	}
	return value, nil
}
