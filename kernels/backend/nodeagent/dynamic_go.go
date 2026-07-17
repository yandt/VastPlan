package nodeagent

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"

	"cdsoft.com.cn/VastPlan/shared/go/pluginid"
	"cdsoft.com.cn/VastPlan/shared/go/protocolbus"
)

// DynamicGoModuleLoader 隔离标准库 plugin 的平台差异，也允许测试注入不执行本机
// 动态链接的验证器。生产实现仍会校验真实 .so 的构建信息与导出模块。
type DynamicGoModuleLoader interface {
	Load(path, pluginID, version, signedFingerprint string) (protocolbus.EmbeddedPlugin, error)
}

type systemDynamicGoLoader struct {
	hostFingerprint string
	mu              sync.Mutex
	loaded          map[string]string
}

func NewDynamicGoLoader(hostFingerprint string) DynamicGoModuleLoader {
	return &systemDynamicGoLoader{
		hostFingerprint: strings.TrimSpace(hostFingerprint), loaded: map[string]string{},
	}
}

func (l *systemDynamicGoLoader) Load(path, pluginID, version, signedFingerprint string) (protocolbus.EmbeddedPlugin, error) {
	if l.hostFingerprint == "" {
		return protocolbus.EmbeddedPlugin{}, errors.New("Backend 未注入 dynamic-go 构建指纹，动态内嵌已安全禁用")
	}
	// Go plugin.Open 会在返回前执行模块 init。签名清单中的指纹必须先于
	// plugin.Open 校验，不能依赖模块自行导出的、执行后才能读取的声明。
	if strings.TrimSpace(signedFingerprint) != l.hostFingerprint {
		return protocolbus.EmbeddedPlugin{}, fmt.Errorf("dynamic-go 签名构建指纹不一致: host=%s artifact=%s",
			l.hostFingerprint, strings.TrimSpace(signedFingerprint))
	}
	path = filepath.Clean(path)
	identity := version + "\x00" + path
	l.mu.Lock()
	defer l.mu.Unlock()
	if previous, exists := l.loaded[pluginID]; exists && previous != identity {
		return protocolbus.EmbeddedPlugin{}, fmt.Errorf("dynamic-go 不支持进程内热替换 %s；已加载 %s，候选 %s，必须滚动重启 Backend",
			pluginID, previous, identity)
	}
	definition, err := loadDynamicGo(path, pluginID, version, l.hostFingerprint)
	if err != nil {
		return protocolbus.EmbeddedPlugin{}, err
	}
	l.loaded[pluginID] = identity
	return definition, nil
}

func validateInProcessFirstParty(plugin InstalledPlugin) error {
	if plugin.Publisher != pluginid.FirstPartyPublisher {
		return fmt.Errorf("dynamic-go 只允许 publisher=%s，实际为 %s", pluginid.FirstPartyPublisher, plugin.Publisher)
	}
	if _, err := pluginid.ParseFirstParty(plugin.ID); err != nil {
		return fmt.Errorf("dynamic-go 只允许已分类的首方插件: %w", err)
	}
	return nil
}
