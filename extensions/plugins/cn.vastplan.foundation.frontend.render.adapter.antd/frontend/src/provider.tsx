import { StyleProvider } from "@ant-design/cssinjs";
import { App as AntdApp, ConfigProvider, theme } from "antd";
import enUS from "antd/locale/en_US";
import zhCN from "antd/locale/zh_CN";
import { useLayoutEffect, useMemo, useRef, useState } from "react";
import type { ReactNode } from "react";
import { PortalUIProvider, VastPlanIcon } from "@vastplan/ui-primitives";
import type { PortalUI } from "@vastplan/ui-primitives";
import { iconButtonWith } from "./feedback";
import { AntdNativeIcon } from "./native-icons";
import { antdPortalUIComponents } from "./portal-ui";
import { antdIconTheme, antdThemeTemplate } from "./theme";

export function antdIconForTheme(id: string | undefined) { return antdIconTheme(id).source === "renderer-native" ? AntdNativeIcon : VastPlanIcon; }

interface ProviderProps { children: ReactNode; locale: string; direction: "ltr" | "rtl"; themeTemplate?: string; iconTheme?: string; }

export function AntdProvider({ children, locale, direction, themeTemplate, iconTheme }: ProviderProps) {
  const boundary = useRef<HTMLDivElement>(null);
  const [styleContainer, setStyleContainer] = useState<Element | ShadowRoot>();
  const activeTemplate = antdThemeTemplate(themeTemplate);
  const activeIconTheme = antdIconTheme(iconTheme);
  useLayoutEffect(() => {
    if (boundary.current === null) return;
    const root = boundary.current.getRootNode();
    setStyleContainer(typeof ShadowRoot !== "undefined" && root instanceof ShadowRoot ? root : boundary.current);
  }, []);
  const popupContainer = () => {
    if (boundary.current === null) throw new Error("Ant Design overlay root 尚未挂载");
    return boundary.current;
  };
  return <div ref={boundary} data-vastplan-design-system="antd" data-vastplan-theme-template={activeTemplate.id} data-vastplan-icon-theme={activeIconTheme.id} lang={locale} dir={direction}>
    {styleContainer === undefined ? null : <StyleProvider container={styleContainer}><ConfigProvider
      locale={locale.toLowerCase().startsWith("zh") ? zhCN : enUS}
      direction={direction}
      getPopupContainer={popupContainer}
      theme={{ algorithm: activeTemplate.scheme === "dark" ? theme.darkAlgorithm : theme.defaultAlgorithm, cssVar: { key: "vastplan" }, token: { borderRadius: 6, controlHeight: 36 } }}
    ><AntdApp><AntdPortalRuntime themeMode={activeTemplate.scheme === "dark" ? "dark" : "light"} iconTheme={activeIconTheme.id}>{children}</AntdPortalRuntime></AntdApp></ConfigProvider></StyleProvider>}
  </div>;
}

function AntdPortalRuntime({ children, themeMode, iconTheme }: { children: ReactNode; themeMode: "light" | "dark"; iconTheme: string }) {
  const { modal, notification } = AntdApp.useApp();
  const ActiveIcon = antdIconForTheme(iconTheme);
  const ui = useMemo<PortalUI>(() => ({
    ...antdPortalUIComponents,
    Icon: ActiveIcon,
    IconButton: (props) => iconButtonWith(ActiveIcon, props),
    notify: ({ title, content, kind = "info" }) => {
      const options = { message: title, description: content };
      if (kind === "success") notification.success(options);
      else if (kind === "warning") notification.warning(options);
      else if (kind === "error") notification.error(options);
      else notification.info(options);
    },
    confirm: ({ title, content }) => new Promise((resolve) => modal.confirm({ title, content, onOk: () => resolve(true), onCancel: () => resolve(false) })),
    theme: { ...antdPortalUIComponents.theme, mode: themeMode },
  }), [ActiveIcon, modal, notification, themeMode]);
  return <PortalUIProvider ui={ui}>{children}</PortalUIProvider>;
}
