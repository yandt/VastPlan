import type { ComponentSize } from "@vastplan/ui-primitives";

export const antdComponentSize = Object.freeze({ sm: "small", md: "middle", lg: "large" } as const satisfies Record<ComponentSize, "small" | "middle" | "large">);
