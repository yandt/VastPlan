// Package pluginid 定义可由内核与插件共同解析的插件命名空间语义。
package pluginid

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

const (
	FirstPartyPrefix    = "cn.vastplan."
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

// ManagementClass 表示插件由哪一类配置入口管理。它只决定配置权限，
// 不替代 Manifest 依赖图、readiness 或运行时启动顺序。
type ManagementClass string

const (
	ManagementPlatform    ManagementClass = "platform"
	ManagementApplication ManagementClass = "application"
	ManagementDevelopment ManagementClass = "development"
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
		return errors.New("cn.vastplan 插件命名空间只允许 publisher=vastplan")
	}
	if publisher == FirstPartyPublisher && !IsFirstPartyID(id) {
		return errors.New("publisher=vastplan 必须使用 cn.vastplan 插件命名空间")
	}
	return nil
}

// ClassifyManagement 在发布者与命名空间所有权校验通过后，按可信身份推导固有管理分类。
// 分类不能由 Manifest 自报，避免应用插件把自己伪装成平台基线。历史未分层首方 ID 和
// example 命名空间只允许用于显式开发模式。
func ClassifyManagement(id, publisher string) (ManagementClass, error) {
	if err := ValidatePublisherOwnership(id, publisher); err != nil {
		return "", err
	}
	if !IsFirstPartyID(id) {
		return ManagementApplication, nil
	}
	namespace, err := ParseFirstParty(id)
	if err != nil {
		return ManagementDevelopment, nil
	}
	switch namespace.Layer {
	case LayerFoundation, LayerPlatform:
		return ManagementPlatform, nil
	case LayerProduct, LayerIntegration:
		return ManagementApplication, nil
	case LayerExample:
		return ManagementDevelopment, nil
	default:
		return "", fmt.Errorf("未知首方插件管理层级 %q", namespace.Layer)
	}
}

// Namespace 把 cn.vastplan.<layer>.<category...>.<component> 拆成机器可判定字段。
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
		return Namespace{}, errors.New("不是 cn.vastplan 首方插件命名空间")
	}
	parts := strings.Split(id, ".")
	if len(parts) < 5 || parts[0] != "cn" || parts[1] != "vastplan" {
		return Namespace{}, errors.New("首方插件 ID 必须使用 cn.vastplan.<layer>.<category...>.<component>")
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
// 这里只做功能分类；cn.vastplan 命名空间所有权由 Manifest 发布者绑定强制。
func (n Namespace) IsPlatformBootstrapReader() bool {
	return n.Layer == LayerFoundation || n.Layer == LayerPlatform
}
