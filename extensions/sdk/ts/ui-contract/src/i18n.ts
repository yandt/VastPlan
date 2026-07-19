export type LocaleDirection = "ltr" | "rtl";
export type MessageValue = string | number | boolean | null | Date;
export type MessageValues = Readonly<Record<string, MessageValue>>;

/** Framework-neutral text reference. The host, never the plugin, chooses the active locale. */
export interface MessageDescriptor {
  namespace: string;
  key: string;
  fallback: string;
  values?: MessageValues;
}

export type LocalizedText = string | MessageDescriptor;
export type LocaleMessages = Readonly<Record<string, string>>;

/** Locale resources carried inside the already verified single-file plugin module. */
export interface PluginLocalization {
  defaultLocale: string;
  messages: Readonly<Record<string, LocaleMessages>>;
}

/** Platform-owned policy resolved into PortalSpec. */
export interface PortalLocalizationPolicy {
  defaultLocale: string;
  supportedLocales: readonly string[];
}

export function canonicalLocale(value: string): string | undefined {
  try {
    return Intl.getCanonicalLocales(value.trim())[0];
  } catch {
    return undefined;
  }
}

export function localeDirection(locale: string): LocaleDirection {
  const language = canonicalLocale(locale)?.split("-")[0];
  return language !== undefined && new Set(["ar", "ckb", "dv", "fa", "he", "ku", "ps", "sd", "ug", "ur", "yi"]).has(language) ? "rtl" : "ltr";
}

/** Exact BCP-47 match first, then same-language match, then the governed default. */
export function resolveLocale(policy: PortalLocalizationPolicy, candidates: readonly string[]): string {
  const supported = [...new Set(policy.supportedLocales.map(canonicalLocale).filter((value): value is string => value !== undefined))];
  const fallback = canonicalLocale(policy.defaultLocale) ?? "zh-CN";
  if (!supported.includes(fallback)) supported.push(fallback);
  for (const raw of candidates) {
    const candidate = canonicalLocale(raw);
    if (candidate === undefined) continue;
    if (supported.includes(candidate)) return candidate;
    const language = candidate.split("-")[0];
    const languageMatch = supported.find((value) => value.split("-")[0] === language);
    if (languageMatch !== undefined) return languageMatch;
  }
  return fallback;
}

export function message(namespace: string, key: string, fallback: string, values?: MessageValues): MessageDescriptor {
  return Object.freeze({ namespace, key, fallback, ...(values === undefined ? {} : { values: Object.freeze({ ...values }) }) });
}
