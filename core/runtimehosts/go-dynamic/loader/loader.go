// Package loader is the only production package allowed to call Go
// plugin.Open. Keeping it below the Go Runtime Host prevents the Backend kernel
// binary from retaining a dynamic module loading path.
package loader

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"

	"cdsoft.com.cn/VastPlan/core/shared/go/protocolbus"
)

type Loader struct {
	hostFingerprint string
	mu              sync.Mutex
	loaded          map[string]string
}

func New(hostFingerprint string) *Loader {
	return &Loader{hostFingerprint: strings.TrimSpace(hostFingerprint), loaded: map[string]string{}}
}

func (l *Loader) Load(path, pluginID, version, signedFingerprint string) (protocolbus.EmbeddedPlugin, error) {
	if l.hostFingerprint == "" {
		return protocolbus.EmbeddedPlugin{}, errors.New("Go Runtime Host 未注入 dynamic-go 构建指纹，加载已安全禁用")
	}
	if strings.TrimSpace(signedFingerprint) != l.hostFingerprint {
		return protocolbus.EmbeddedPlugin{}, fmt.Errorf("dynamic-go 签名构建指纹不一致: host=%s artifact=%s",
			l.hostFingerprint, strings.TrimSpace(signedFingerprint))
	}
	path = filepath.Clean(path)
	identity := version + "\x00" + path
	l.mu.Lock()
	defer l.mu.Unlock()
	if previous, exists := l.loaded[pluginID]; exists && previous != identity {
		return protocolbus.EmbeddedPlugin{}, fmt.Errorf("dynamic-go 不支持 Host 内热替换 %s；已加载 %s，候选 %s，必须创建新 generation",
			pluginID, previous, identity)
	}
	definition, err := load(path, pluginID, version, l.hostFingerprint)
	if err != nil {
		return protocolbus.EmbeddedPlugin{}, err
	}
	l.loaded[pluginID] = identity
	return definition, nil
}
