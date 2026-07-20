package databaseruntime

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"time"
	"unicode"
)

type ProviderSecurityPolicy struct {
	AllowInsecureTLS bool
}

type providerOptions struct {
	User             string `json:"user"`
	TLSMode          string `json:"tlsMode,omitempty"`
	ServerName       string `json:"serverName,omitempty"`
	ConnectTimeoutMS int64  `json:"connectTimeoutMs,omitempty"`
	ApplicationName  string `json:"applicationName,omitempty"`
	Network          string `json:"network,omitempty"`
	ReadTimeoutMS    int64  `json:"readTimeoutMs,omitempty"`
	WriteTimeoutMS   int64  `json:"writeTimeoutMs,omitempty"`
	RejectReadOnly   bool   `json:"rejectReadOnly,omitempty"`
}

func decodeProviderOptions(raw json.RawMessage, target *providerOptions) error {
	if target == nil {
		return errors.New("Provider options 目标不能为空")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("解析 Provider options: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return errors.New("Provider options 只能包含一个 JSON 对象")
	}
	if target.User != strings.TrimSpace(target.User) || target.TLSMode != strings.TrimSpace(target.TLSMode) ||
		target.ServerName != strings.TrimSpace(target.ServerName) || target.ApplicationName != strings.TrimSpace(target.ApplicationName) ||
		target.Network != strings.TrimSpace(target.Network) {
		return errors.New("Provider options 字符串不得包含首尾空白")
	}
	if target.User == "" || len(target.User) > 128 || strings.IndexFunc(target.User, invalidControlRune) >= 0 {
		return errors.New("Provider user 无效")
	}
	if target.TLSMode == "" {
		target.TLSMode = "verify-full"
	}
	if target.TLSMode != "verify-full" && target.TLSMode != "disable" {
		return errors.New("tlsMode 仅支持 verify-full 或 disable")
	}
	if len(target.ServerName) > 253 || strings.IndexFunc(target.ServerName, invalidControlRune) >= 0 ||
		strings.IndexFunc(target.ServerName, unicode.IsSpace) >= 0 {
		return errors.New("serverName 无效")
	}
	if target.ConnectTimeoutMS == 0 {
		target.ConnectTimeoutMS = 10_000
	}
	if err := validateProviderTimeout("connectTimeoutMs", target.ConnectTimeoutMS, 100); err != nil {
		return err
	}
	if err := validateProviderTimeout("readTimeoutMs", target.ReadTimeoutMS, 0); err != nil {
		return err
	}
	if err := validateProviderTimeout("writeTimeoutMs", target.WriteTimeoutMS, 0); err != nil {
		return err
	}
	if len(target.ApplicationName) > 128 || strings.IndexFunc(target.ApplicationName, invalidControlRune) >= 0 {
		return errors.New("applicationName 无效")
	}
	return nil
}

func validateProviderTimeout(name string, value, minimum int64) error {
	if value < minimum || value > int64((5*time.Minute)/time.Millisecond) {
		return fmt.Errorf("%s 超出允许范围", name)
	}
	return nil
}

func invalidControlRune(value rune) bool { return value < 0x20 || value == 0x7f }

func enforceTLSMode(options providerOptions, policy ProviderSecurityPolicy) error {
	if options.TLSMode == "disable" && !policy.AllowInsecureTLS {
		return errors.New("部署策略禁止关闭数据库 TLS 校验")
	}
	return nil
}

func tcpEndpoint(endpoint string, defaultPort uint16) (host string, port uint16, err error) {
	if strings.HasPrefix(endpoint, "/") {
		return "", 0, errors.New("当前 Provider 配置要求 TCP endpoint")
	}
	host, encodedPort, splitErr := net.SplitHostPort(endpoint)
	if splitErr != nil {
		if strings.Contains(endpoint, ":") {
			return "", 0, fmt.Errorf("endpoint 必须使用 host:port，IPv6 必须使用方括号: %w", splitErr)
		}
		host, encodedPort = endpoint, strconv.Itoa(int(defaultPort))
	}
	host = strings.TrimSpace(host)
	if host == "" || strings.IndexFunc(host, invalidControlRune) >= 0 {
		return "", 0, errors.New("endpoint host 无效")
	}
	parsedPort, parseErr := strconv.ParseUint(encodedPort, 10, 16)
	if parseErr != nil || parsedPort == 0 {
		return "", 0, errors.New("endpoint port 无效")
	}
	return host, uint16(parsedPort), nil
}
