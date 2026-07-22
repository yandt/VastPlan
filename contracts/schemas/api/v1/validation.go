package apiv1

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base32"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

const (
	maxInlineSchemaBytes = 64 << 10
	maxEndpointLease     = 5 * time.Minute
)

var routeKeyEncoding = base32.StdEncoding.WithPadding(base32.NoPadding)

func NewRouteKey() (string, error) {
	raw := make([]byte, 12)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("生成 API Exposure Route Key: %w", err)
	}
	return strings.ToLower(routeKeyEncoding.EncodeToString(raw)), nil
}

func NewExposureID() (string, error) {
	key, err := NewRouteKey()
	if err != nil {
		return "", err
	}
	return "exp_" + key, nil
}

func NewDataPlaneExposureID() (string, error) {
	key, err := NewRouteKey()
	if err != nil {
		return "", err
	}
	return "dpx_" + key, nil
}

func ValidateContractContribution(contract ContractContribution) error {
	document, err := jsonDocument(contract)
	if err != nil {
		return err
	}
	if err := validateDefinition("apiContractContribution", document); err != nil {
		return err
	}
	seenIDs := map[string]struct{}{}
	seenRoutes := map[string]struct{}{}
	for index, route := range contract.Routes {
		if _, duplicate := seenIDs[route.ID]; duplicate {
			return fmt.Errorf("API Contract route id 重复: %s", route.ID)
		}
		seenIDs[route.ID] = struct{}{}
		key := route.Method + "\x00" + route.Path
		if _, duplicate := seenRoutes[key]; duplicate {
			return fmt.Errorf("API Contract 路由重复: %s %s", route.Method, route.Path)
		}
		seenRoutes[key] = struct{}{}
		if err := validateInlineSchema(route.RequestSchema); err != nil {
			return fmt.Errorf("API Contract route %d requestSchema: %w", index, err)
		}
		if err := validateInlineSchema(route.ResponseSchema); err != nil {
			return fmt.Errorf("API Contract route %d responseSchema: %w", index, err)
		}
		seenErrors := map[string]struct{}{}
		if err := validateRouteParameterNames(route.Path); err != nil {
			return fmt.Errorf("API Contract route %s: %w", route.ID, err)
		}
		for _, mapping := range route.Errors {
			if _, duplicate := seenErrors[mapping.Code]; duplicate {
				return fmt.Errorf("API Contract route %s 错误映射重复: %s", route.ID, mapping.Code)
			}
			seenErrors[mapping.Code] = struct{}{}
		}
	}
	return nil
}

func ValidateDataPlaneService(service DataPlaneServiceContribution) error {
	document, err := jsonDocument(service)
	if err != nil {
		return err
	}
	return validateDefinition("dataPlaneServiceContribution", document)
}

func ContractDigest(contract ContractContribution) (string, error) {
	if err := ValidateContractContribution(contract); err != nil {
		return "", err
	}
	canonical := contract
	canonical.Routes = append([]RouteContract(nil), contract.Routes...)
	for index := range canonical.Routes {
		canonical.Routes[index].Errors = append([]ErrorMapping(nil), canonical.Routes[index].Errors...)
		sort.Slice(canonical.Routes[index].Errors, func(left, right int) bool {
			return canonical.Routes[index].Errors[left].Code < canonical.Routes[index].Errors[right].Code
		})
	}
	sort.Slice(canonical.Routes, func(left, right int) bool {
		return canonical.Routes[left].ID < canonical.Routes[right].ID
	})
	raw, err := json.Marshal(canonical)
	if err != nil {
		return "", err
	}
	var document any
	if err := json.Unmarshal(raw, &document); err != nil {
		return "", err
	}
	var normalized bytes.Buffer
	encoder := json.NewEncoder(&normalized)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(document); err != nil {
		return "", err
	}
	digest := sha256.Sum256(bytes.TrimSuffix(normalized.Bytes(), []byte{'\n'}))
	return hex.EncodeToString(digest[:]), nil
}

func ValidateExposure(exposure Exposure) error {
	document, err := jsonDocument(exposure)
	if err != nil {
		return err
	}
	if err := validateDefinition("exposure", document); err != nil {
		return err
	}
	for _, host := range exposure.Hosts {
		if host != strings.ToLower(host) || strings.HasSuffix(host, ".") {
			return fmt.Errorf("API Exposure host 必须规范化为小写且无尾点: %s", host)
		}
	}
	return nil
}

func ValidateExposureCatalog(catalog ExposureCatalog) error {
	document, err := jsonDocument(catalog)
	if err != nil {
		return err
	}
	if err := validateDefinition("exposureCatalog", document); err != nil {
		return err
	}
	seenIDs := map[string]struct{}{}
	seenKeys := map[string]struct{}{}
	for _, resolved := range catalog.Exposures {
		exposure := resolved.Exposure
		if err := ValidateExposure(exposure); err != nil {
			return fmt.Errorf("API Exposure %s: %w", exposure.ID, err)
		}
		if err := validateResolvedContract(resolved); err != nil {
			return fmt.Errorf("API Exposure %s: %w", exposure.ID, err)
		}
		if _, duplicate := seenIDs[exposure.ID]; duplicate {
			return fmt.Errorf("API Exposure id 重复: %s", exposure.ID)
		}
		seenIDs[exposure.ID] = struct{}{}
		if _, duplicate := seenKeys[exposure.RouteKey]; duplicate {
			return fmt.Errorf("API Exposure routeKey 冲突: %s", exposure.RouteKey)
		}
		seenKeys[exposure.RouteKey] = struct{}{}
	}
	return nil
}

func ValidateDataPlaneExposure(exposure DataPlaneExposure) error {
	document, err := jsonDocument(exposure)
	if err != nil {
		return err
	}
	return validateDefinition("dataPlaneExposure", document)
}

func ValidateEndpointLease(lease EndpointLease, now time.Time) error {
	document, err := jsonDocument(lease)
	if err != nil {
		return err
	}
	if err := validateDefinition("endpointLease", document); err != nil {
		return err
	}
	endpoint, err := url.Parse(lease.Endpoint)
	if err != nil || endpoint.Scheme != "https" || endpoint.Host == "" || endpoint.User != nil || endpoint.RawQuery != "" || endpoint.Fragment != "" {
		return errors.New("Data Plane Endpoint Lease 必须使用无凭据、无 query/fragment 的 HTTPS endpoint")
	}
	if lease.IssuedAt.After(now.Add(30*time.Second)) || !lease.ExpiresAt.After(now) || !lease.ExpiresAt.After(lease.IssuedAt) || lease.ExpiresAt.Sub(lease.IssuedAt) > maxEndpointLease {
		return errors.New("Data Plane Endpoint Lease 时间窗无效")
	}
	return nil
}

func ValidateGatewayInvocation(invocation GatewayInvocation) error {
	document, err := jsonDocument(invocation)
	if err != nil {
		return err
	}
	return validateDefinition("gatewayInvocation", document)
}

func ResolveExposure(catalog ExposureCatalog, host, routeKey string, majorVersion uint64) (ResolvedExposure, bool) {
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	for _, resolved := range catalog.Exposures {
		exposure := resolved.Exposure
		if exposure.RouteKey != routeKey || contractMajor(exposure.Contract.ContractVersion) != majorVersion {
			continue
		}
		for _, allowed := range exposure.Hosts {
			if allowed == host {
				return resolved, true
			}
		}
	}
	return ResolvedExposure{}, false
}

func contractMajor(version string) uint64 {
	var major uint64
	_, _ = fmt.Sscanf(strings.SplitN(version, ".", 2)[0], "%d", &major)
	return major
}

func validateResolvedContract(resolved ResolvedExposure) error {
	if err := ValidateContractContribution(resolved.Contract); err != nil {
		return err
	}
	reference := resolved.Exposure.Contract
	if reference.ContributionID != resolved.Contract.ID ||
		reference.ContractID != resolved.Contract.ContractID ||
		reference.ContractVersion != resolved.Contract.ContractVersion {
		return errors.New("已解析契约与 Exposure 契约引用不一致")
	}
	digest, err := ContractDigest(resolved.Contract)
	if err != nil {
		return err
	}
	if digest != reference.ContractDigest {
		return errors.New("已解析契约摘要与 Exposure 契约引用不一致")
	}
	return nil
}

func validateInlineSchema(raw json.RawMessage) error {
	if len(raw) == 0 || len(raw) > maxInlineSchemaBytes {
		return fmt.Errorf("内联 JSON Schema 大小必须为 1..%d bytes", maxInlineSchemaBytes)
	}
	var value any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		return fmt.Errorf("解析内联 JSON Schema: %w", err)
	}
	root, ok := value.(map[string]any)
	if !ok {
		return errors.New("内联 JSON Schema 根必须是对象")
	}
	if ref := externalReference(root); ref != "" {
		return fmt.Errorf("内联 JSON Schema 不得引用外部资源: %s", ref)
	}
	compiler := jsonschema.NewCompiler()
	const resource = "urn:vastplan:api:inline-schema"
	if err := compiler.AddResource(resource, root); err != nil {
		return fmt.Errorf("登记内联 JSON Schema: %w", err)
	}
	if _, err := compiler.Compile(resource); err != nil {
		return fmt.Errorf("编译内联 JSON Schema: %w", err)
	}
	return nil
}

func validateRouteParameterNames(path string) error {
	seen := map[string]struct{}{}
	for _, segment := range strings.Split(path, "/") {
		if !strings.HasPrefix(segment, "{") {
			continue
		}
		name := strings.TrimSuffix(strings.TrimPrefix(segment, "{"), "}")
		if _, duplicate := seen[name]; duplicate {
			return fmt.Errorf("path 参数重复: %s", name)
		}
		seen[name] = struct{}{}
	}
	return nil
}

func externalReference(value any) string {
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			if key == "$ref" {
				if ref, ok := child.(string); ok && !strings.HasPrefix(ref, "#") {
					return ref
				}
			}
			if ref := externalReference(child); ref != "" {
				return ref
			}
		}
	case []any:
		for _, child := range typed {
			if ref := externalReference(child); ref != "" {
				return ref
			}
		}
	}
	return ""
}

func jsonDocument(value any) (any, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	document, err := jsonschemaDocument(raw)
	if err != nil {
		return nil, err
	}
	return document, nil
}

func jsonschemaDocument(raw []byte) (any, error) {
	var value any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	return value, nil
}
