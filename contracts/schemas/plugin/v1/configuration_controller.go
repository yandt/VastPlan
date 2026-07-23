package pluginv1

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
)

const (
	ConfigurationControllerExtensionPoint = "configuration.controller"
	ConfigurationControllerProtocol       = "configuration.v1"
	configurationControllerPrefix         = "configuration."
)

// ConfigurationControllerCapability derives a stable opaque capability from
// the signed plugin identity. It prevents controller routing from exposing the
// author-maintained plugin ID as a public API alias while still allowing every
// language SDK to derive exactly the same target.
func ConfigurationControllerCapability(pluginID string) (string, error) {
	pluginID = strings.TrimSpace(pluginID)
	if pluginID == "" {
		return "", errors.New("配置控制器缺少插件身份")
	}
	digest := sha256.Sum256([]byte(pluginID))
	return configurationControllerPrefix + hex.EncodeToString(digest[:16]), nil
}

func configurationControllerDescriptor() []byte {
	return []byte(`{"protocol":"configuration.v1"}`)
}
