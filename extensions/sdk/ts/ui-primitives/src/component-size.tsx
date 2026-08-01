import { createContext, createElement, useContext, type ReactNode } from "react";
import { componentSizes, type ComponentSize, type SizeableProps } from "@vastplan/ui-contract";

/** 所有 Workbench 组件未显式指定时使用标准中档。 */
export const defaultComponentSize: ComponentSize = "md";

const componentSizeContext = createContext<ComponentSize>(defaultComponentSize);

export function resolveComponentSize(size: ComponentSize | undefined, inherited: ComponentSize = defaultComponentSize): ComponentSize {
  const resolved = size ?? inherited;
  if (!componentSizes.includes(resolved)) throw new Error(`组件 size 无效: ${String(resolved)}`);
  return resolved;
}

/** 在组合根选择一次尺寸，后续组件只消费统一上下文并允许局部覆盖。 */
export function ComponentSizeProvider({ size, children }: SizeableProps & { children?: ReactNode }) {
  const inherited = useContext(componentSizeContext);
  return createElement(componentSizeContext.Provider, { value: resolveComponentSize(size, inherited) }, children);
}

export function useComponentSize(size?: ComponentSize): ComponentSize {
  return resolveComponentSize(size, useContext(componentSizeContext));
}
