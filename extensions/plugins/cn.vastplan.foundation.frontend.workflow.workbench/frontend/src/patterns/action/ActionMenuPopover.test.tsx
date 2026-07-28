import { renderToStaticMarkup } from "react-dom/server";
import type { ReactNode } from "react";
import { describe, expect, it, vi } from "vitest";

vi.mock("@vastplan/ui-primitives", () => ({
  usePortalUI: () => ({
    theme: { tokens: { color: { danger: "#d00", mutedText: "#777" } } },
    IconButton: ({ icon, label, size }: { icon: string; label: string; size?: string }) => <button type="button" data-icon={icon} data-size={size} aria-label={label} />,
    Icon: ({ name }: { name: string }) => <span data-icon-glyph={name} />,
    Menu: ({ size, variant, items }: { size?: string; variant?: string; items: Array<{ id: string; label: ReactNode; icon?: ReactNode; disabled?: boolean }> }) => <div data-menu data-size={size} data-variant={variant}>{items.map((item) => <button key={item.id} type="button" disabled={item.disabled} data-menu-item={item.id}>{item.icon}{item.label}</button>)}</div>,
    Popover: ({ children, initialFocus, placement, surface, trigger }: { children: ReactNode; initialFocus?: string; placement?: string; surface?: string; trigger(props: { ref: undefined; "aria-expanded": false; "aria-controls": string; onClick(): void; onKeyDown(): void }): ReactNode }) => <div data-popover data-focus={initialFocus} data-placement={placement} data-surface={surface}>{trigger({ ref: undefined, "aria-expanded": false, "aria-controls": "action-menu", onClick: () => undefined, onKeyDown: () => undefined })}{children}</div>,
  }),
}));

import { ActionMenuPopover } from "./ActionMenuPopover.js";

describe("ActionMenuPopover", () => {
  it("owns the compact data-driven action menu presentation", () => {
    const html = renderToStaticMarkup(<ActionMenuPopover
      label="More actions"
      items={[
        { id: "publish", label: "Publish", icon: "publish" },
        { id: "remove", label: "Remove", icon: "remove", tone: "danger" },
        { id: "locked", label: "Locked", icon: "settings", disabled: true },
      ]}
      onSelect={() => undefined}
    />);
    expect(html).toContain('data-popover="true"');
    expect(html).toContain('data-focus="first"');
    expect(html).toContain('data-placement="bottom-end"');
    expect(html).toContain('data-surface="compact"');
    expect(html).toContain('aria-label="More actions"');
    expect(html).toContain('data-size="sm"');
    expect(html).toContain('data-variant="action"');
    expect(html).toContain('data-menu-item="publish"');
    expect(html).toContain('data-icon-glyph="remove"');
    expect(html).toContain('color:#d00');
    expect(html).toContain('color:#777');
    expect(html).toContain('text-overflow:ellipsis');
    expect(html).toContain('title="Publish"');
  });
});
