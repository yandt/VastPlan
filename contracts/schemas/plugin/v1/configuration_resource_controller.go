package pluginv1

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
)

const (
	ConfigurationResourceControllerExtensionPoint = "configuration.resource-controller"
	ConfigurationResourceControllerProtocol       = "configuration.resource.v1"
	configurationResourceControllerPrefix         = "configuration.resource."
	configurationResourceCollectionPrefix         = "cfgc_"
)

// ConfigurationResourceControllerCapability is internal routing identity. It
// is deterministic across Go/Node/Python SDKs but does not reveal a plugin ID.
func ConfigurationResourceControllerCapability(pluginID string) (string, error) {
	pluginID = strings.TrimSpace(pluginID)
	if pluginID == "" {
		return "", errors.New("配置资源控制器缺少插件身份")
	}
	digest := sha256.Sum256([]byte(pluginID))
	return configurationResourceControllerPrefix + hex.EncodeToString(digest[:16]), nil
}

// ConfigurationResourceCollectionID is the opaque public identity of one
// signed collection contract. Service routing still selects the exact unit.
func ConfigurationResourceCollectionID(pluginID, collectionID string) (string, error) {
	pluginID, collectionID = strings.TrimSpace(pluginID), strings.TrimSpace(collectionID)
	if pluginID == "" || collectionID == "" {
		return "", errors.New("配置资源集合身份无效")
	}
	digest := sha256.Sum256([]byte(fmt.Sprintf("%d:%s\n%d:%s\n", len(pluginID), pluginID, len(collectionID), collectionID)))
	return configurationResourceCollectionPrefix + hex.EncodeToString(digest[:12]), nil
}

func configurationResourceControllerDescriptor() []byte {
	return []byte(`{"protocol":"configuration.resource.v1"}`)
}
