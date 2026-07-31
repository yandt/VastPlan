import { createContext, useContext, type PropsWithChildren } from "react";
import type { PortalAccountSummary } from "./appearance.js";
import type { UIShellProps } from "./shell.js";

export type PortalPersonalization = Pick<UIShellProps,
  "appearance" |
  "availableTemplates" | "template" | "onTemplateChange" |
  "renderers" | "renderer" | "onRendererChange" |
  "iconThemes" | "iconThemeID" | "onIconThemeChange" |
  "onAppearanceChange"
> & { account: PortalAccountSummary };

const PortalPersonalizationContext = createContext<PortalPersonalization | undefined>(undefined);

/** Trusted host port consumed by account-oriented frontend plugins. */
export function PortalPersonalizationProvider({ value, children }: PropsWithChildren<{ value: PortalPersonalization }>) {
  return <PortalPersonalizationContext.Provider value={value}>{children}</PortalPersonalizationContext.Provider>;
}

export function usePortalPersonalization(): PortalPersonalization {
  const value = useContext(PortalPersonalizationContext);
  if (value === undefined) throw new Error("Portal personalization host 尚未挂载");
  return value;
}
