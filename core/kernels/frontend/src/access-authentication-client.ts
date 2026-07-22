import type { ModuleFetcher } from "./module-loader";

export interface AccessBootstrap {
  readonly schemaVersion: "v1";
  readonly generationId: string;
  readonly accessTemplate: string;
  readonly localization: { readonly defaultLocale: string; readonly supportedLocales: readonly string[] };
  readonly authentication: { readonly allowedMethods: readonly string[]; readonly defaultMethod: string; readonly reuseIdentifier: boolean };
  readonly branding: { readonly productName: Readonly<Record<string, string>>; readonly logoAssetId?: string; readonly supportPath?: string; readonly privacyPath?: string };
}

export interface AuthenticationMethod {
  readonly methodId: string;
  readonly interaction: "form" | "redirect" | "native";
  readonly displayName: Readonly<Record<string, string>>;
}

export interface AuthenticationField {
  readonly id: string;
  readonly kind: "identifier" | "password" | "one-time-code" | "select";
  readonly label: Readonly<Record<string, string>>;
  readonly help: Readonly<Record<string, string>>;
  readonly autocomplete: string;
  readonly required: boolean;
  readonly minLength: number;
  readonly maxLength: number;
  readonly choices: readonly { readonly value: string; readonly label: Readonly<Record<string, string>> }[];
}

export interface AuthenticationStep {
  readonly stepId: string;
  readonly kind: "identifier" | "password" | "one-time-code" | "redirect" | "context-selection";
  readonly title: Readonly<Record<string, string>>;
  readonly description: Readonly<Record<string, string>>;
  readonly submitLabel: Readonly<Record<string, string>>;
  readonly fields: readonly AuthenticationField[];
  readonly redirectUri?: string;
  readonly expiresAt: string;
  readonly resendAfter?: string;
}

export interface AuthenticationResult { readonly state: string; readonly step?: AuthenticationStep; readonly reasonCode?: string; }

export class AccessAuthenticationClient {
  private csrf?: string;
  public constructor(private readonly fetcher: ModuleFetcher, private readonly returnTo: string) {}

  public async bootstrap(): Promise<{ access: AccessBootstrap; methods: readonly AuthenticationMethod[]; defaultMethod: string }> {
    const [accessResponse, methodsResponse] = await Promise.all([
      this.fetcher(`/auth/v1/bootstrap?returnTo=${encodeURIComponent(this.returnTo)}`, { credentials: "same-origin", cache: "no-store" }),
      this.fetcher(`/auth/v1/methods?returnTo=${encodeURIComponent(this.returnTo)}`, { credentials: "same-origin", cache: "no-store" }),
    ]);
    if (!accessResponse.ok || !methodsResponse.ok) throw new Error("authentication.bootstrap_unavailable");
    const access = await accessResponse.json() as AccessBootstrap;
    const methods = await methodsResponse.json() as { methods: readonly AuthenticationMethod[]; defaultMethod: string };
    if (access.schemaVersion !== "v1" || !Array.isArray(methods.methods) || methods.methods.length === 0) throw new Error("authentication.bootstrap_invalid");
    return { access, methods: methods.methods, defaultMethod: methods.defaultMethod };
  }

  public async bootstrapProviderTest(methodId: string): Promise<{ access: AccessBootstrap; methods: readonly AuthenticationMethod[]; defaultMethod: string }> {
    const response = await this.fetcher(`/auth/v1/bootstrap?returnTo=${encodeURIComponent(this.returnTo)}`, { credentials: "same-origin", cache: "no-store" });
    if (!response.ok) throw new Error("authentication.bootstrap_unavailable");
    const access = await response.json() as AccessBootstrap;
    if (access.schemaVersion !== "v1") throw new Error("authentication.bootstrap_invalid");
    return { access, methods: [{ methodId, interaction: "form", displayName: { "zh-CN": "Provider 认证测试", "en-US": "Provider authentication test" } }], defaultMethod: methodId };
  }

  public async begin(methodId: string, locale: string): Promise<{ transactionId: string; result: AuthenticationResult }> {
    return this.mutate("/auth/v1/transactions", "POST", { methodId, locale, returnTo: this.returnTo });
  }

  public async beginProviderTest(providerProfileId: string, methodId: string, locale: string): Promise<{ transactionId: string; result: AuthenticationResult }> {
    return this.mutate("/auth/v1/provider-tests", "POST", { providerProfileId, methodId, locale, returnTo: this.returnTo });
  }

  public async continue(transactionId: string, stepId: string, responses: readonly { fieldId: string; value: string }[]): Promise<{ transactionId: string; result: AuthenticationResult; returnTo?: string }> {
    return this.mutate(`/auth/v1/transactions/${encodeURIComponent(transactionId)}/continue`, "POST", { stepId, responses });
  }

  public async resend(transactionId: string): Promise<{ transactionId: string; result: AuthenticationResult }> {
    return this.mutate(`/auth/v1/transactions/${encodeURIComponent(transactionId)}/resend`, "POST", {});
  }

  public async cancel(transactionId: string): Promise<void> {
    await this.mutate(`/auth/v1/transactions/${encodeURIComponent(transactionId)}`, "DELETE", {});
  }

  private async mutate<T>(path: string, method: "POST" | "DELETE", body: unknown): Promise<T> {
    if (this.csrf === undefined) {
      const response = await this.fetcher("/auth/v1/csrf", { credentials: "same-origin", cache: "no-store" });
      const value = await response.json() as { token?: string };
      if (!response.ok || typeof value.token !== "string") throw new Error("authentication.csrf_unavailable");
      this.csrf = value.token;
    }
    const response = await this.fetcher(path, { method, credentials: "same-origin", cache: "no-store", headers: { "Content-Type": "application/json", "X-VastPlan-CSRF": this.csrf }, body: JSON.stringify(body) });
    const value = response.status === 204 ? undefined : await response.json();
    if (!response.ok) throw new Error(typeof value === "object" && value !== null && "error" in value ? String(value.error) : "authentication.request_rejected");
    return value as T;
  }
}

export function accessReturnTo(location: Pick<Location, "search"> | undefined): string {
  if (location === undefined) return "/";
  const value = new URLSearchParams(location.search).get("returnTo") ?? "/";
  return value.startsWith("/") && !value.startsWith("//") && value.length <= 2048 && !/[\0\r\n\\]/.test(value) ? value : "/";
}

export function providerTestSelection(location: Pick<Location, "search"> | undefined): { providerProfileId: string; methodId: string } | undefined {
  if (location === undefined) return undefined;
  const params = new URLSearchParams(location.search), providerProfileId = params.get("providerTest"), methodId = params.get("method");
  return safeID(providerProfileId) && safeID(methodId) ? { providerProfileId, methodId } : undefined;
}

export function accessBrandAssetURL(access: AccessBootstrap, returnTo: string): string | undefined {
  const id = access.branding.logoAssetId;
  if (id === undefined || !/^[a-z][a-z0-9._-]{0,127}$/.test(id) || !/^[a-f0-9]{64}$/.test(access.generationId)) return undefined;
  return `/auth/v1/assets/${access.generationId}/${encodeURIComponent(id)}?returnTo=${encodeURIComponent(returnTo)}`;
}

export function accessLocaleDirection(locale: string): "ltr" | "rtl" {
  return /^(ar|fa|he|ur)(?:-|$)/i.test(locale) ? "rtl" : "ltr";
}

export function localizeAccessText(value: Readonly<Record<string,string>> | undefined, locale: string, fallback: string): string {
  return value?.[locale] ?? value?.[locale.split("-")[0]] ?? value?.["en-US"] ?? value?.["zh-CN"] ?? value?.[Object.keys(value)[0] ?? ""] ?? fallback;
}

function safeID(value: string | null): value is string { return value !== null && /^[A-Za-z0-9][A-Za-z0-9._:@/-]{0,255}$/.test(value); }
