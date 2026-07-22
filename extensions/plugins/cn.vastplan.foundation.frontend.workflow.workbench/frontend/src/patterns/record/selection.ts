export function initialRecordSelection(parameter: string): string | undefined {
  try {
    const value = new URLSearchParams(globalThis.location?.search ?? "").get(parameter);
    return value === null || value === "" || value.length > 240 ? undefined : value;
  } catch { return undefined; }
}

export function persistRecordSelection(parameter: string, value: string | undefined): void {
  try {
    if (globalThis.location === undefined || globalThis.history === undefined) return;
    const url = new URL(globalThis.location.href);
    if (value === undefined) url.searchParams.delete(parameter); else url.searchParams.set(parameter, value);
    globalThis.history.replaceState(globalThis.history.state, "", `${url.pathname}${url.search}${url.hash}`);
  } catch { /* Embedded or privacy-constrained browsers may deny history access. */ }
}
