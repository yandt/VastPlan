import type { ComponentSize } from "@vastplan/ui-primitives";

export const antdComponentSize = Object.freeze({ xs: "small", sm: "small", md: "middle", lg: "large" } as const satisfies Record<ComponentSize, "small" | "middle" | "large">);
