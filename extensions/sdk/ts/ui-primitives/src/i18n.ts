import { createContext, createElement, useCallback, useContext, useEffect, useMemo, useState } from "react";
import type { ReactNode } from "react";
import {
  canonicalLocale,
  localeDirection,
  message,
  resolveLocale,
  type LocalizedText,
  type LocaleDirection,
  type MessageDescriptor,
  type MessageValues,
  type PluginLocalization,
  type PortalLocalizationPolicy,
} from "@vastplan/ui-contract";

export interface PortalI18n {
  readonly locale: string;
  readonly direction: LocaleDirection;
  readonly supportedLocales: readonly string[];
  text(value: LocalizedText): string;
  message(namespace: string, key: string, fallback: string, values?: MessageValues): MessageDescriptor;
  setLocale(locale: string): void;
  formatDate(value: Date | number | string, options?: Intl.DateTimeFormatOptions): string;
  formatNumber(value: number, options?: Intl.NumberFormatOptions): string;
  formatList(value: readonly string[], options?: Intl.ListFormatOptions): string;
  formatRelativeTime(value: number, unit: Intl.RelativeTimeFormatUnit, options?: Intl.RelativeTimeFormatOptions): string;
}

export type PortalMessageCatalogs = Readonly<Record<string, PluginLocalization>>;

export interface PortalI18nProviderProps {
  policy: PortalLocalizationPolicy;
  catalogs: PortalMessageCatalogs;
  candidates?: readonly string[];
  storageKey?: string;
  children?: ReactNode;
}

const context = createContext<PortalI18n | null>(null);

export function PortalI18nProvider({ policy, catalogs, candidates = [], storageKey, children }: PortalI18nProviderProps) {
  const normalizedPolicy = useMemo(() => normalizePolicy(policy), [policy]);
  const stored = storageKey === undefined ? undefined : safeStorageGet(storageKey);
  const [locale, setLocaleState] = useState(() => resolveLocale(normalizedPolicy, [...(stored === undefined ? [] : [stored]), ...candidates]));
  useEffect(() => {
    setLocaleState((current) => resolveLocale(normalizedPolicy, [current, ...candidates]));
  }, [normalizedPolicy, candidates]);
  const setLocale = useCallback((next: string) => {
    const resolved = resolveLocale(normalizedPolicy, [next]);
    setLocaleState(resolved);
    if (storageKey !== undefined) safeStorageSet(storageKey, resolved);
  }, [normalizedPolicy, storageKey]);
  const text = useCallback((value: LocalizedText) => translate(value, locale, catalogs), [catalogs, locale]);
  const value = useMemo<PortalI18n>(() => ({
    locale,
    direction: localeDirection(locale),
    supportedLocales: normalizedPolicy.supportedLocales,
    text,
    message,
    setLocale,
    formatDate: (input, options) => new Intl.DateTimeFormat(locale, options).format(asDate(input)),
    formatNumber: (input, options) => new Intl.NumberFormat(locale, options).format(input),
    formatList: (input, options) => new Intl.ListFormat(locale, options).format(input),
    formatRelativeTime: (input, unit, options) => new Intl.RelativeTimeFormat(locale, options).format(input, unit),
  }), [locale, normalizedPolicy.supportedLocales, setLocale, text]);
  useEffect(() => {
    if (typeof document === "undefined") return;
    document.documentElement.lang = value.locale;
    document.documentElement.dir = value.direction;
  }, [value.direction, value.locale]);
  return createElement(context.Provider, { value }, children);
}

export function usePortalI18n(): PortalI18n {
  const value = useContext(context);
  if (value === null) throw new Error("Portal i18n is not initialized");
  return value;
}

export function usePortalMessages(namespace: string): (key: string, fallback: string, values?: MessageValues) => string {
  const i18n = usePortalI18n();
  return useCallback((key, fallback, values) => i18n.text(message(namespace, key, fallback, values)), [i18n, namespace]);
}

export function translate(value: LocalizedText, locale: string, catalogs: PortalMessageCatalogs): string {
  if (typeof value === "string") return value;
  const catalog = catalogs[value.namespace];
  const template = catalog === undefined ? undefined : findMessage(catalog, locale, value.key);
  return interpolate(template ?? value.fallback, value.values);
}

export function localizeJSONSchema(schema: Readonly<Record<string, unknown>>, localization: Readonly<Record<string, LocalizedText>> | undefined, text: (value: LocalizedText) => string): Record<string, unknown> {
  const copy = cloneValue(schema) as Record<string, unknown>;
  for (const [pointer, descriptor] of Object.entries(localization ?? {})) setJSONPointer(copy, pointer, text(descriptor));
  return copy;
}

function normalizePolicy(policy: PortalLocalizationPolicy): PortalLocalizationPolicy {
  const defaultLocale = canonicalLocale(policy.defaultLocale) ?? "zh-CN";
  const supportedLocales = [...new Set([...policy.supportedLocales, defaultLocale].map(canonicalLocale).filter((value): value is string => value !== undefined))];
  return Object.freeze({ defaultLocale, supportedLocales: Object.freeze(supportedLocales) });
}

function findMessage(catalog: PluginLocalization, locale: string, key: string): string | undefined {
  const locales = Object.keys(catalog.messages);
  const exact = canonicalLocale(locale);
  const sameLanguage = exact === undefined ? undefined : locales.find((candidate) => canonicalLocale(candidate)?.split("-")[0] === exact.split("-")[0]);
  const fallback = canonicalLocale(catalog.defaultLocale);
  for (const candidate of [exact, sameLanguage, fallback, "zh-CN"]) {
    if (candidate === undefined) continue;
    const match = locales.find((item) => canonicalLocale(item) === candidate);
    const value = match === undefined ? undefined : catalog.messages[match]?.[key];
    if (value !== undefined) return value;
  }
  return undefined;
}

function interpolate(template: string, values: MessageValues | undefined): string {
  if (values === undefined) return template;
  return template.replace(/\{([A-Za-z0-9_.-]+)\}/g, (token, key: string) => {
    const value = values[key];
    return value === undefined ? token : value instanceof Date ? value.toISOString() : String(value);
  });
}

function asDate(value: Date | number | string): Date {
  const date = value instanceof Date ? value : new Date(value);
  return Number.isNaN(date.valueOf()) ? new Date(0) : date;
}

function safeStorageGet(key: string): string | undefined {
  try { return globalThis.localStorage?.getItem(key) ?? undefined; } catch { return undefined; }
}

function safeStorageSet(key: string, value: string): void {
  try { globalThis.localStorage?.setItem(key, value); } catch { /* preference persistence is best effort */ }
}

function cloneValue(value: unknown): unknown {
  if (Array.isArray(value)) return value.map(cloneValue);
  if (typeof value === "object" && value !== null) return Object.fromEntries(Object.entries(value).map(([key, item]) => [key, cloneValue(item)]));
  return value;
}

function setJSONPointer(root: Record<string, unknown>, pointer: string, value: string): void {
  if (!pointer.startsWith("/") || pointer === "/") return;
  const parts = pointer.slice(1).split("/").map((part) => part.replace(/~1/g, "/").replace(/~0/g, "~"));
  let current: unknown = root;
  for (const part of parts.slice(0, -1)) {
    if (Array.isArray(current)) {
      const index = arrayIndex(part, current.length);
      if (index === undefined) return;
      current = current[index];
      continue;
    }
    if (typeof current !== "object" || current === null || !Object.hasOwn(current, part)) return;
    current = (current as Record<string, unknown>)[part];
  }
  const last = parts[parts.length - 1];
  if (last === undefined) return;
  if (Array.isArray(current)) {
    const index = arrayIndex(last, current.length);
    if (index !== undefined) current[index] = value;
    return;
  }
  if (typeof current === "object" && current !== null && Object.hasOwn(current, last)) (current as Record<string, unknown>)[last] = value;
}

function arrayIndex(value: string, length: number): number | undefined {
  if (!/^(0|[1-9][0-9]*)$/.test(value)) return undefined;
  const index = Number(value);
  return Number.isSafeInteger(index) && index < length ? index : undefined;
}
