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
)

//go:embed vastplan.platform-control.schema.json
var schemaJSON []byte

var compiled struct {
	sync.Once
	schema *jsonschema.Schema
	err    error
}

func ParseProfile(raw []byte) (Profile, error) {
	compiled.Do(func() {
		compiler := jsonschema.NewCompiler()
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
	if profile.SchemaVersion != Version || profile.Generation == 0 {
		return errors.New("Platform Control Profile 版本或 generation 无效")
	}
	host, portText, err := net.SplitHostPort(profile.Endpoint)
	port, portErr := strconv.Atoi(portText)
	if err != nil || portErr != nil || host == "" || port < 1 || port > 65535 || strings.ContainsAny(profile.Endpoint, "@/\\\r\n\t") {
		return errors.New("Platform Control endpoint 必须是 host:port")
	}
	if profile.TLS.Mode != "disable" && profile.TLS.Mode != "verify-ca" && profile.TLS.Mode != "verify-full" {
		return errors.New("Platform Control TLS mode 无效")
	}
	if profile.TLS.Mode == "verify-full" && strings.TrimSpace(profile.TLS.ServerName) == "" {
		return errors.New("verify-full 必须声明 serverName")
	}
	if !safeName(profile.Database) || !safeName(profile.Schema) || !safeName(profile.Username) {
		return errors.New("Platform Control database、schema 或 username 无效")
	}
	if _, err := semver.NewConstraint(profile.ContractRange); err != nil {
		return fmt.Errorf("Platform Control contractRange 无效: %w", err)
	}
	return ValidateSecretRef(profile.SecretRef)
}

func ValidateSecretRef(ref SecretRef) error {
	switch ref.Kind {
	case "systemd-credential":
		if !safeName(ref.Name) || ref.Path != "" {
			return errors.New("systemd credential 引用无效")
		}
	case "owner-file":
		if ref.Name != "" || !filepath.IsAbs(ref.Path) || filepath.Clean(ref.Path) != ref.Path {
			return errors.New("owner-file secret 引用必须是规范绝对路径")
		}
	default:
		return errors.New("Platform Control secret provider 无效")
	}
	return nil
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
