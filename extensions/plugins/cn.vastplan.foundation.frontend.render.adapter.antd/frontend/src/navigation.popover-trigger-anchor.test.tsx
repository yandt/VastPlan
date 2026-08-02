import { isValidElement, type ReactNode } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it, vi } from "vitest";

vi.mock("antd", async (importOriginal) => {
  const actual = await importOriginal<typeof import("antd")>();
  return { ...actual, Popover: ({ children }: { children: ReactNode }) => <div data-trigger-anchor={isValidElement(children) ? children.type : "missing"}>{children}</div> };
});

import { Popover } from "./navigation.js";

function PluginTrigger() { return <button type="button">用户</button>; }

describe("Ant Design Popover trigger anchor", () => {
  it("wraps plugin trigger components in a DOM anchor so overlays do not depend on ref forwarding", () => {
    const markup = renderToStaticMarkup(<Popover open={false} onOpenChange={() => undefined} trigger={() => <PluginTrigger />}>菜单</Popover>);
    expect(markup).toContain('data-trigger-anchor="span"');
    expect(markup).toContain('class="vp-antd-popover-trigger-anchor"');
  });
});
