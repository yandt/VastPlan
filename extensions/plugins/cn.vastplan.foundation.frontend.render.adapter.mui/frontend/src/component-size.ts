import type { ComponentSize } from "@vastplan/ui-primitives";

export const muiComponentSize = Object.freeze({ sm: "small", md: "medium", lg: "large" } as const satisfies Record<ComponentSize, "small" | "medium" | "large">);
