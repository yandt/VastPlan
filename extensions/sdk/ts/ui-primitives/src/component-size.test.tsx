import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";
import { ComponentSizeProvider, defaultComponentSize, resolveComponentSize, useComponentSize } from "./component-size.js";

function Probe({ size }: { size?: "xs" | "sm" | "md" | "lg" }) {
  return <span data-size={useComponentSize(size)} />;
}

describe("ComponentSizeProvider", () => {
  it("uses md by default and lets the nearest explicit size win", () => {
    expect(defaultComponentSize).toBe("md");
    expect(resolveComponentSize(undefined)).toBe("md");
    const markup = renderToStaticMarkup(<><Probe /><ComponentSizeProvider size="sm"><Probe /><Probe size="xs" /></ComponentSizeProvider></>);
    expect(markup).toContain('data-size="md"');
    expect(markup).toContain('data-size="sm"');
    expect(markup).toContain('data-size="xs"');
  });

  it("rejects bypassed runtime values through the shared policy", () => {
    expect(() => resolveComponentSize("tiny" as never)).toThrow("组件 size 无效");
  });
});
