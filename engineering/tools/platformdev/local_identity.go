package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"cdsoft.com.cn/VastPlan/core/kernels/backend/pluginservice"
)

func writeSessions(filename string, ownerPermissions []string) error {
	type record struct {
		TokenSHA256 string   `json:"tokenSHA256"`
		ID          string   `json:"id"`
		TenantID    string   `json:"tenantId"`
		Roles       []string `json:"roles"`
		ExpiresAt   string   `json:"expiresAt"`
	}
	ownerRoles := append([]string{"portal.read", "portal.compose", "portal.approve", "portal.publish"}, ownerPermissions...)
	sort.Strings(ownerRoles)
	sessions := []struct {
		token, id string
		roles     []string
	}{
		{devAdminToken, "local-admin", ownerRoles},
		{authorToken, "local-author", []string{"portal.read", "portal.compose", "platform.deployment.read", "platform.deployment.compose"}},
		{approverToken, "local-approver", []string{"portal.read", "portal.approve", "platform.deployment.read", "platform.deployment.approve"}},
		{publisherToken, "local-publisher", []string{"portal.read", "portal.publish", "platform.deployment.read", "platform.deployment.publish"}},
	}
	doc := struct {
		Sessions []record `json:"sessions"`
	}{}
	for _, session := range sessions {
		digest := sha256.Sum256([]byte(session.token))
		doc.Sessions = append(doc.Sessions, record{
			TokenSHA256: hex.EncodeToString(digest[:]), ID: session.id, TenantID: "local", Roles: session.roles,
			ExpiresAt: time.Now().Add(24 * time.Hour).UTC().Format(time.RFC3339),
		})
	}
	raw, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filename, append(raw, '\n'), 0o600)
}

func ensurePrivateDirectory(path string) error {
	if err := os.MkdirAll(path, 0o700); err != nil {
		return err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf("%s 必须是普通目录且不能是符号链接", path)
	}
	if info.Mode().Perm()&0o077 != 0 {
		return fmt.Errorf("%s 权限过宽 %o，要求 0700 或更严格", path, info.Mode().Perm())
	}
	return nil
}

func ensureSigningIdentity(privateFilename, publisher, keyID string) (pluginservice.TrustKey, error) {
	if strings.TrimSpace(publisher) == "" || strings.TrimSpace(keyID) == "" {
		return pluginservice.TrustKey{}, errors.New("签名身份 publisher 和 keyId 不能为空")
	}
	if err := ensurePrivateDirectory(filepath.Dir(privateFilename)); err != nil {
		return pluginservice.TrustKey{}, err
	}
	info, err := os.Lstat(privateFilename)
	if errors.Is(err, os.ErrNotExist) {
		_, privateKey, generateErr := ed25519.GenerateKey(rand.Reader)
		if generateErr != nil {
			return pluginservice.TrustKey{}, generateErr
		}
		encoded, marshalErr := pluginservice.MarshalEd25519PrivateKeyPEM(privateKey)
		if marshalErr != nil {
			return pluginservice.TrustKey{}, marshalErr
		}
		file, createErr := os.OpenFile(privateFilename, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if createErr == nil {
			written, writeErr := file.Write(encoded)
			if writeErr == nil && written != len(encoded) {
				writeErr = io.ErrShortWrite
			}
			syncErr := file.Sync()
			closeErr := file.Close()
			if writeErr != nil || syncErr != nil || closeErr != nil {
				_ = os.Remove(privateFilename)
				return pluginservice.TrustKey{}, errors.Join(writeErr, syncErr, closeErr)
			}
		} else if !errors.Is(createErr, os.ErrExist) {
			return pluginservice.TrustKey{}, createErr
		}
		info, err = os.Lstat(privateFilename)
	}
	if err != nil {
		return pluginservice.TrustKey{}, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return pluginservice.TrustKey{}, fmt.Errorf("签名私钥 %s 必须是普通文件且不能是符号链接", privateFilename)
	}
	if info.Mode().Perm()&0o077 != 0 {
		return pluginservice.TrustKey{}, fmt.Errorf("签名私钥 %s 权限过宽 %o，要求 0600 或更严格", privateFilename, info.Mode().Perm())
	}
	privateKey, err := pluginservice.LoadEd25519PrivateKeyPEM(privateFilename)
	if err != nil {
		return pluginservice.TrustKey{}, err
	}
	publicKey, ok := privateKey.Public().(ed25519.PublicKey)
	if !ok {
		return pluginservice.TrustKey{}, errors.New("签名私钥无法导出 Ed25519 公钥")
	}
	return pluginservice.TrustKey{
		Publisher: publisher, KeyID: keyID, PublicKey: base64.StdEncoding.EncodeToString(publicKey),
	}, nil
}

func writeTrustDocument(filename string, keys ...pluginservice.TrustKey) error {
	document := pluginservice.TrustDocumentForPublicKeys(keys...)
	raw, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filename, append(raw, '\n'), 0o600)
}

func writeTLS(certFile, keyFile string) error {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return err
	}
	template := x509.Certificate{
		SerialNumber: serial, Subject: pkix.Name{CommonName: "vastplan-local-development"},
		DNSNames: []string{"localhost"}, IPAddresses: []net.IP{net.ParseIP("127.0.0.1")},
		NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(24 * time.Hour),
		KeyUsage:    x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	if err != nil {
		return err
	}
	if err := os.WriteFile(certFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600); err != nil {
		return err
	}
	return os.WriteFile(keyFile, pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}), 0o600)
}

func splitAddress(address string) (string, int, error) {
	host, rawPort, err := net.SplitHostPort(address)
	if err != nil {
		return "", 0, err
	}
	var port int
	if _, err := fmt.Sscanf(rawPort, "%d", &port); err != nil || port < 0 {
		return "", 0, fmt.Errorf("非法端口 %q", rawPort)
	}
	if host != "127.0.0.1" && host != "localhost" {
		return "", 0, errors.New("开发服务只允许监听 loopback")
	}
	return host, port, nil
}

func mergedEnv(extra map[string]string) []string {
	values := map[string]string{}
	for _, item := range os.Environ() {
		if index := strings.IndexByte(item, '='); index > 0 {
			values[item[:index]] = item[index+1:]
		}
	}
	for key, value := range extra {
		values[key] = value
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	result := make([]string, 0, len(keys))
	for _, key := range keys {
		result = append(result, key+"="+values[key])
	}
	return result
}

func insecureLocalTLS() *tls.Config {
	return &tls.Config{InsecureSkipVerify: true, MinVersion: tls.VersionTLS12} // #nosec G402 -- generated loopback-only development certificate.
}
