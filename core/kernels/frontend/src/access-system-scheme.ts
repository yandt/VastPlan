import { useEffect, useState } from "react";

export type AccessSystemScheme = "light" | "dark";

/** Resolves the only browser-local appearance signal allowed before a session exists. */
export function resolveAccessSystemScheme(): AccessSystemScheme {
  return globalThis.matchMedia?.("(prefers-color-scheme: dark)").matches === true ? "dark" : "light";
}

/** Keeps the public access facade synchronized with operating-system appearance changes. */
export function useAccessSystemScheme(): AccessSystemScheme {
  const [scheme, setScheme] = useState<AccessSystemScheme>(resolveAccessSystemScheme);
  useEffect(() => {
    const media = globalThis.matchMedia?.("(prefers-color-scheme: dark)");
    if (media === undefined) return;
    const update = () => setScheme(media.matches ? "dark" : "light");
    media.addEventListener("change", update);
    return () => media.removeEventListener("change", update);
  }, []);
  return scheme;
}
