import type { ComponentSize } from "@vastplan/ui-primitives";

export const arcoComponentSize = Object.freeze({ sm: "mini", md: "default", lg: "large" } as const satisfies Record<ComponentSize, "mini" | "small" | "default" | "large">);
