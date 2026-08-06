package platformcontrolv1

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	semver "github.com/Masterminds/semver/v3"
	"github.com/santhosh-tekuri/jsonschema/v6"

	databasev1 "cdsoft.com.cn/VastPlan/contracts/schemas/database/v1"
)

//go:embed vastplan.platform-control.schema.json
var schemaJSON []byte

var compiled struct {
	sync.Once
	schema *jsonschema.Schema
	err    error
}

// ValidationIssue is the only Platform Control validation detail allowed to
// cross process boundaries. Field and reason are stable identifiers; candidate
// values and provider errors are deliberately excluded.
type ValidationIssue struct {
	Field  string
	Reason string
}

func (e *ValidationIssue) Error() string {
	if e == nil {
		return "Platform Control 配置无效"
	}
	return validationIssueMessage(e.Field, e.Reason)
}

func ValidationIssueFrom(err error) (ValidationIssue, bool) {
	var issue *ValidationIssue
	if !errors.As(err, &issue) || issue == nil {
		return ValidationIssue{}, false
	}
	return *issue, true
}

func invalid(field, reason string) error { return &ValidationIssue{Field: field, Reason: reason} }

func ParseProfile(raw []byte) (Profile, error) {
	compiled.Do(func() {
		compiler := jsonschema.NewCompiler()
		if err := databasev1.AddResources(compiler); err != nil {
			compiled.err = err
			return
		}
		document, err := jsonschema.UnmarshalJSON(bytes.NewReader(schemaJSON))
		if err == nil {
			err = compiler.AddResource(SchemaURL, document)
		}
		if err == nil {
			compiled.schema, err = compiler.Compile(SchemaURL + "#/$defs/profile")
		}
		compiled.err = err
	})
	if compiled.err != nil {
		return Profile{}, fmt.Errorf("编译 Platform Control Profile Schema: %w", compiled.err)
	}
	instance, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		return Profile{}, err
	}
	if err := compiled.schema.Validate(instance); err != nil {
		return Profile{}, fmt.Errorf("Platform Control Profile 不符合 Schema: %w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var profile Profile
	if err := decoder.Decode(&profile); err != nil {
		return Profile{}, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return Profile{}, errors.New("Platform Control Profile 只能包含一个 JSON 文档")
	}
	if err := ValidateProfile(profile); err != nil {
		return Profile{}, err
	}
	return profile, nil
}

func ValidateProfile(profile Profile) error {
	if err := validateProfileCore(profile); err != nil {
		return err
	}
	return ValidateSecretRef(profile.SecretRef)
}

func validateProfileCore(profile Profile) error {
	if profile.SchemaVersion != Version || profile.Generation == 0 {
		return invalid("profile.generation", "invalid")
	}
	if err := databasev1.ValidateConnectionCandidate(profile.Connection); err != nil {
		return err
	}
	if profile.Connection.ProviderID != "postgresql" && profile.Connection.ProviderID != "mysql" {
		return invalid("profile.connection.providerId", "unsupported")
	}
	host, portText, err := net.SplitHostPort(profile.Connection.Endpoint)
	port, portErr := strconv.Atoi(portText)
	if err != nil || portErr != nil || host == "" || port < 1 || port > 65535 || strings.ContainsAny(profile.Connection.Endpoint, "@/\\\r\n\t") {
		return invalid("profile.connection.endpoint", "host_port_required")
	}
	if !safeName(profile.Connection.Database) {
		return invalid("profile.connection.database", "identifier_required")
	}
	identifierLimit := 64
	if profile.Connection.ProviderID == "postgresql" {
		identifierLimit = 63
	}
	if len(profile.Connection.Database) > identifierLimit {
		return invalid("profile.connection.database", "provider_length_exceeded")
	}
	var options map[string]any
	if err := json.Unmarshal(profile.Connection.Options, &options); err != nil {
		return invalid("profile.connection.options", "object_required")
	}
	username, _ := options["user"].(string)
	if !safeName(username) {
		return invalid("profile.connection.options.user", "identifier_required")
	}
	tlsMode, _ := options["tlsMode"].(string)
	if tlsMode != "disable" && tlsMode != "verify-ca" && tlsMode != "verify-full" {
		return invalid("profile.connection.options.tlsMode", "unsupported")
	}
	serverName, _ := options["serverName"].(string)
	if tlsMode == "verify-full" && strings.TrimSpace(serverName) == "" {
		return invalid("profile.connection.options.serverName", "required")
	}
	if !safeName(profile.Schema) {
		return invalid("profile.schema", "identifier_required")
	}
	if len(profile.Schema) > identifierLimit {
		return invalid("profile.schema", "provider_length_exceeded")
	}
	if _, err := semver.NewConstraint(profile.ContractRange); err != nil {
		return invalid("profile.contractRange", "semver_range_required")
	}
	return nil
}

// ValidateChangeRequest keeps plaintext material outside the durable Profile.
// Exactly one secret input is accepted: inline material for trusted staging or
// an already provisioned external reference.
func ValidateChangeRequest(request ChangeRequest) error {
	if request.Profile.Generation != request.ExpectedGeneration+1 {
		return invalid("profile.generation", "stale")
	}
	if request.CreateDatabaseIfMissing && request.ExpectedGeneration != 0 {
		return invalid("createDatabaseIfMissing", "initial_only")
	}
	if err := validateProfileCore(request.Profile); err != nil {
		return err
	}
	if request.SecretMaterial != "" {
		if request.Profile.SecretRef != (SecretRef{}) {
			return invalid("secretMaterial", "conflicts_with_reference")
		}
		if len([]byte(request.SecretMaterial)) > MaxSecretMaterialBytes {
			return invalid("secretMaterial", "too_large")
		}
		if strings.ContainsRune(request.SecretMaterial, '\x00') {
			return invalid("secretMaterial", "contains_nul")
		}
		return nil
	}
	return ValidateSecretRef(request.Profile.SecretRef)
}

func ValidateSecretRef(ref SecretRef) error {
	switch ref.Kind {
	case "systemd-credential":
		if !safeName(ref.Name) || ref.Path != "" {
			return invalid("profile.secretRef.name", "identifier_required")
		}
	case "owner-file":
		if ref.Name != "" || !filepath.IsAbs(ref.Path) || filepath.Clean(ref.Path) != ref.Path {
			return invalid("profile.secretRef.path", "absolute_path_required")
		}
	default:
		return invalid("profile.secretRef.kind", "unsupported")
	}
	return nil
}

func validationIssueMessage(field, reason string) string {
	switch field + ":" + reason {
	case "profile.generation:invalid", "profile.generation:stale":
		return "配置代次已变化，请刷新页面后重试"
	case "profile.connection.providerId:unsupported":
		return "平台控制数据库仅支持 PostgreSQL 或 MySQL"
	case "profile.connection.endpoint:host_port_required":
		return "数据库地址必须包含有效主机和 1–65535 端口"
	case "profile.connection.database:identifier_required":
		return "数据库名称只能包含字母、数字、点、下划线和连字符"
	case "profile.connection.database:provider_length_exceeded":
		return "数据库名称超过当前数据库类型的标识符长度上限"
	case "profile.connection.options.user:identifier_required":
		return "用户名只能包含字母、数字、点、下划线和连字符"
	case "profile.connection.options.tlsMode:unsupported":
		return "传输加密模式不受支持"
	case "profile.connection.options.serverName:required":
		return "完整校验模式必须填写证书校验服务器名称"
	case "profile.schema:identifier_required":
		return "Schema 名称只能包含字母、数字、点、下划线和连字符"
	case "profile.schema:provider_length_exceeded":
		return "Schema 名称超过当前数据库类型的标识符长度上限"
	case "profile.contractRange:semver_range_required":
		return "能力契约范围必须是有效的 SemVer 范围"
	case "secretMaterial:conflicts_with_reference":
		return "密码与外部密钥引用不能同时提交"
	case "secretMaterial:too_large":
		return "密码长度超过 64 KiB 上限"
	case "secretMaterial:contains_nul":
		return "密码不能包含空字符"
	case "createDatabaseIfMissing:initial_only":
		return "仅首次配置平台控制数据库时可以自动创建目标数据库"
	case "profile.secretRef.name:identifier_required":
		return "systemd Credential 名称格式无效"
	case "profile.secretRef.path:absolute_path_required":
		return "密码文件必须使用规范绝对路径"
	case "profile.secretRef.kind:unsupported":
		return "请选择直接输入密码或受支持的外部密钥类型"
	default:
		return "Platform Control 配置无效"
	}
}

func safeName(value string) bool {
	if value == "" || len(value) > 128 || value != strings.TrimSpace(value) {
		return false
	}
	for _, r := range value {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '.' || r == '_' || r == '-' {
			continue
		}
		return false
	}
	return true
}
