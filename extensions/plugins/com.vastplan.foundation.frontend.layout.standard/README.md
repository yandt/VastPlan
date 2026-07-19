# Standard Portal Layout

本基础插件只负责标准 Shell 组合模型的视觉布局：LOGO 放置、图标主轨、常驻二级导航、页头与正文的尺寸、间距、背景和响应方式。

它不定义 Slot 名称、不接受功能插件创建页面骨架，也不决定某个业务组件应出现在哪个 Slot；这些职责属于 `ui.shell-composition` 与功能插件声明。

桌面默认采用 64px 图标主轨 + 240px 常驻二级导航栏。一级图标只切换导航分组，二级页面才触发路由；二级导航不是弹出层。主轨中段、尾段和二级栏各自管理纵向溢出。手机隐藏桌面双栏并使用设计系统 Drawer。

Page Header 位于 Page Body 滚动容器之外。布局区域采用“有实际内容才渲染”的统一规则：Slot 贡献、导航分组以及布局自身放置的 LOGO、页面标题等都属于实际内容。完全为空的 Shell Header、Page Aside 和 Footer 会自动折叠；包含页面标题的 Page Header 不会因扩展 Slot 为空而消失。
