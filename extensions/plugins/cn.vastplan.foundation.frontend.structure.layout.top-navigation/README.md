# Top Navigation Portal Layout

这是单 Shell 插件内部的顶部导航模板源码。它消费统一的 `ui.structure.shell` 语义模型，不定义页面、Slot 或业务菜单，只决定品牌、根导航、页面头部和正文的视觉排列。

桌面使用 64px 顶栏。主菜单、用户菜单和“更多”复用共享 `PortalNavigationMenu` 的弹层模式，当前由 Ant Design Renderer 提供整行 hover、选中和子菜单交互；标准侧栏使用同一组件的内联模式，因此顶部布局只负责 Popover、根菜单位置和溢出策略，不复刻框架菜单样式。主导航与辅助导航位于中间区域，设置导航位于右侧。空间不足时，非活动根导航进入“更多”，活动根导航始终优先保留。

手机隐藏桌面顶栏导航并使用设计系统 Drawer。Page Header 位于 Page Body 滚动容器之外，因此正文溢出时页头保持可见。所有 Popover 的定位、碰撞处理、ESC、外部点击和焦点恢复由当前设计系统适配器负责。
