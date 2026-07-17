// Package pluginid 定义可由内核与插件共同解析的插件命名空间语义。
package pluginid

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

const (
	FirstPartyPrefix    = "com.vastplan."
	FirstPartyPublisher = "vastplan"
)

// Layer 是首方插件在启动依赖和产品边界中的稳定层级。
type Layer string

const (
	LayerFoundation  Layer = "foundation"
	LayerPlatform    Layer = "platform"
	LayerProduct     Layer = "product"
	LayerIntegration Layer = "integration"
	LayerExample     Layer = "example"
)

var (
	segmentPattern = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)
	layers         = map[Layer]struct{}{
		LayerFoundation: {}, LayerPlatform: {}, LayerProduct: {},
		LayerIntegration: {}, LayerExample: {},
	}
)

// IsFirstPartyID 判断 ID 是否声明占用 VastPlan 首方命名空间。
func IsFirstPartyID(id string) bool { return strings.HasPrefix(id, FirstPartyPrefix) }

// ValidatePublisherOwnership 双向绑定首方命名空间与发布者身份。它只校验
// 命名空间所有权；制品签名和完整性仍由制品信任链负责。
func ValidatePublisherOwnership(id, publisher string) error {
	if IsFirstPartyID(id) && publisher != FirstPartyPublisher {
		return errors.New("com.vastplan 插件命名空间只允许 publisher=vastplan")
	}
	if publisher == FirstPartyPublisher && !IsFirstPartyID(id) {
		return errors.New("publisher=vastplan 必须使用 com.vastplan 插件命名空间")
	}
	return nil
}

// Namespace 把 com.vastplan.<layer>.<category...>.<component> 拆成机器可判定字段。
// Categories 至少包含一级，既可表达 security 等大功能域，也可继续细分为
// data.relational 等子域；最后一段始终是具体组件名。
type Namespace struct {
	Layer      Layer
	Categories []string
	Component  string
}

// ParseFirstParty 解析新的首方多级命名空间。历史 demo ID 仍可被通用 Manifest
// Schema 读取，但不会被本函数误判成已分类的生产插件。
func ParseFirstParty(id string) (Namespace, error) {
	if !IsFirstPartyID(id) {
		return Namespace{}, errors.New("不是 com.vastplan 首方插件命名空间")
	}
	parts := strings.Split(id, ".")
	if len(parts) < 5 || parts[0] != "com" || parts[1] != "vastplan" {
		return Namespace{}, errors.New("首方插件 ID 必须使用 com.vastplan.<layer>.<category...>.<component>")
	}
	layer := Layer(parts[2])
	if _, ok := layers[layer]; !ok {
		return Namespace{}, fmt.Errorf("未知首方插件层级 %q", layer)
	}
	for _, segment := range parts[3:] {
		if !segmentPattern.MatchString(segment) {
			return Namespace{}, fmt.Errorf("首方插件命名空间段非法 %q", segment)
		}
	}
	return Namespace{
		Layer:      layer,
		Categories: append([]string(nil), parts[3:len(parts)-1]...),
		Component:  parts[len(parts)-1],
	}, nil
}

// Domain 返回第一级功能分类，适合做粗粒度功能判断。
func (n Namespace) Domain() string {
	if len(n.Categories) == 0 {
		return ""
	}
	return n.Categories[0]
}

// CategoryPath 返回完整功能分类路径，适合做细粒度路由、策略与审计。
func (n Namespace) CategoryPath() string { return strings.Join(n.Categories, ".") }

// IsPlatformBootstrapReader 表示该首方插件是否属于允许读取系统设置的基础层。
// 这里只做功能分类；com.vastplan 命名空间所有权由 Manifest 发布者绑定强制。
func (n Namespace) IsPlatformBootstrapReader() bool {
	return n.Layer == LayerFoundation || n.Layer == LayerPlatform
}
