import { createContext, createElement, useContext, type ReactNode } from "react";
import type { PortalUI } from "./index.js";

const portalUIContext = createContext<PortalUI | null>(null);

export function PortalUIProvider({ ui, children }: { ui: PortalUI; children?: ReactNode }) {
  return createElement(portalUIContext.Provider, { value: ui }, children);
}

/** Obtains the Portal-selected design system without exposing its underlying framework. */
export function usePortalUI(): PortalUI {
  const ui = useContext(portalUIContext);
  if (ui === null) throw new Error("Portal UI 未初始化：功能插件只能在设计系统 Provider 内渲染");
  return ui;
}
