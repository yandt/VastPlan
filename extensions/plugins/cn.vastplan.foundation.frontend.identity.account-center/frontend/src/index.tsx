import {
  accountNavigationGroupID,
  accountPageExtensionPointID,
  accountSettingsNavigationGroupID,
  PortalAccountProfilePage,
  PortalAppearanceSettingsPage,
  type FrontendPluginContext,
} from "@vastplan/ui-primitives";

export const localization = {
  defaultLocale: "zh-CN",
  messages: {
    "zh-CN": {
      "profile.title": "用户信息", "profile.navigation": "用户信息", "profile.summary": "基本信息",
      "profile.name": "名称", "profile.subject": "用户 ID", "profile.tenant": "租户",
      "appearance.title": "外观", "appearance.navigation": "外观", "appearance.summary": "外观设置",
      "appearance.mode": "主题模式", "appearance.system": "跟随系统", "appearance.light": "浅色", "appearance.dark": "深色", "appearance.framework": "UI 框架", "appearance.layout": "页面布局", "appearance.icons": "图标风格",
      "appearance.preferences": "偏好设置", "appearance.preferencesHint": "切换后立即生效", "appearance.theme": "主题与颜色", "appearance.themeHint": "分别配置浅色与深色模式", "appearance.template": "主题模板",
      "theme.light": "浅色经典", "theme.light-soft": "浅色柔和", "theme.light-warm": "浅色暖调", "theme.dark": "深色石墨", "theme.dark-midnight": "深色午夜", "theme.dark-slate": "深色蓝灰",
      "color.canvas": "页面背景", "color.surface": "组件背景", "color.text": "正文", "color.mutedText": "次要文字", "color.border": "边框", "color.primary": "强调色", "color.danger": "危险", "color.warning": "警告", "color.success": "成功",
      "appearance.localOnly": "外观只保存在当前浏览器，修改后即时生效且不会上传到服务器。"
    },
    "en-US": {
      "profile.title": "Profile", "profile.navigation": "Profile", "profile.summary": "Basic information",
      "profile.name": "Name", "profile.subject": "User ID", "profile.tenant": "Tenant",
      "appearance.title": "Appearance", "appearance.navigation": "Appearance", "appearance.summary": "Appearance settings",
      "appearance.mode": "Theme mode", "appearance.system": "System", "appearance.light": "Light", "appearance.dark": "Dark", "appearance.framework": "UI framework", "appearance.layout": "Page layout", "appearance.icons": "Icon style",
      "appearance.preferences": "Preferences", "appearance.preferencesHint": "Changes take effect immediately", "appearance.theme": "Theme and colors", "appearance.themeHint": "Configure light and dark modes separately", "appearance.template": "Theme template",
      "theme.light": "Classic light", "theme.light-soft": "Soft light", "theme.light-warm": "Warm light", "theme.dark": "Graphite dark", "theme.dark-midnight": "Midnight dark", "theme.dark-slate": "Slate dark",
      "color.canvas": "Page background", "color.surface": "Component surface", "color.text": "Text", "color.mutedText": "Muted text", "color.border": "Border", "color.primary": "Accent", "color.danger": "Danger", "color.warning": "Warning", "color.success": "Success",
      "appearance.localOnly": "Appearance is stored only in this browser, takes effect immediately, and is never uploaded to the server."
    }
  }
} as const;

export default {
  register(context: FrontendPluginContext) {
    if (!context.extensions.owns(accountPageExtensionPointID)) throw new Error("个人中心扩展点未由可信 Portal Runtime 装配");
    context.addPage({
      id: "account.profile",
      path: "/account/profile",
      title: context.i18n.message("profile.title", "用户信息"),
      navigation: {
        id: "account.profile",
        label: context.i18n.message("profile.navigation", "用户信息"),
        zone: "secondary",
        groupID: accountNavigationGroupID,
        order: 10,
      },
      slots: [{ id: "account.profile.body", slot: "page.body.main", component: PortalAccountProfilePage }],
    });
    context.addPage({
      id: "account.settings.appearance",
      path: "/account/settings/appearance",
      title: context.i18n.message("appearance.title", "外观"),
      bodyLayout: "small",
      navigation: {
        id: "account.settings.appearance",
        label: context.i18n.message("appearance.navigation", "外观"),
        zone: "secondary",
        groupID: accountSettingsNavigationGroupID,
        order: 10,
      },
      slots: [{ id: "account.appearance.body", slot: "page.body.main", component: PortalAppearanceSettingsPage }],
    });
  },
  localization,
};
