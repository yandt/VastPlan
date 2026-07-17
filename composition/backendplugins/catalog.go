// Package backendplugins 是 Backend 发布物的组合根。内核通用运行时不 import
// 任何具体插件；只有这里明确列出的代码才会进入最终二进制的静态内嵌目录。
package backendplugins

import (
	"cdsoft.com.cn/VastPlan/kernels/backend/nodeagent"
	bootstrapembedded "cdsoft.com.cn/VastPlan/plugins/com.vastplan.foundation.security.bootstrap-policy/embedded"
)

func DefaultCatalog() (*nodeagent.EmbeddedCatalog, error) {
	return nodeagent.NewEmbeddedCatalog(bootstrapembedded.Definition)
}
