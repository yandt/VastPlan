import { useEffect, useMemo, useState, type FormEvent } from "react";
import { AccessAuthenticationClient, accessBrandAssetURL, accessLocaleDirection, accessReturnTo, localizeAccessText as localized, providerTestSelection, type AccessBootstrap, type AuthenticationMethod, type AuthenticationStep } from "./access-authentication-client";
import { accessAppearance } from "./access-appearance";
import { AccessLocaleSelector } from "./access-locale-selector";
import { AccessMethodSelector } from "./access-method-selector";
import { useAccessSystemScheme } from "./access-system-scheme";
import type { ModuleFetcher } from "./module-loader";

type MessageKey = keyof typeof messages["zh-CN"];

export function AccessLoginPage({ fetcher }: { fetcher: ModuleFetcher }) {
  const returnTo = useMemo(() => accessReturnTo(globalThis.location), []);
  const client = useMemo(() => new AccessAuthenticationClient(fetcher, returnTo), [fetcher, returnTo]);
  const providerTest = useMemo(() => providerTestSelection(globalThis.location), []);
  const [access, setAccess] = useState<AccessBootstrap>();
  const [methods, setMethods] = useState<readonly AuthenticationMethod[]>([]);
  const [methodId, setMethodId] = useState("");
  const [locale, setLocale] = useState("zh-CN");
  const [transactionId, setTransactionId] = useState<string>();
  const [step, setStep] = useState<AuthenticationStep>();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<MessageKey>();
  const [clock, setClock] = useState(Date.now());
  const systemScheme = useAccessSystemScheme();

  useEffect(() => {
    let active = true;
    void (providerTest === undefined ? client.bootstrap() : client.bootstrapProviderTest(providerTest.methodId)).then((value) => {
      if (!active) return;
      const selectedLocale = selectLocale(value.access.localization.supportedLocales, value.access.localization.defaultLocale);
      const testMethod = providerTest === undefined ? undefined : value.methods.find(({ methodId }) => methodId === providerTest.methodId) ?? { methodId: providerTest.methodId, interaction: "form" as const, displayName: { "zh-CN": "Provider 认证测试" } };
      const availableMethods = testMethod === undefined ? value.methods : [testMethod];
      setAccess(value.access); setMethods(availableMethods); setMethodId(availableMethods.length === 1 ? (testMethod?.methodId ?? value.defaultMethod) : ""); setLocale(selectedLocale);
      if (availableMethods.length === 1) {
        const initialMethodID = testMethod?.methodId ?? value.defaultMethod;
        setBusy(true); setError(undefined);
        void beginAuthentication(client, providerTest, initialMethodID, selectedLocale).then((result) => {
          if (active) { setTransactionId(result.transactionId); setStep(result.result.step); }
        }).catch(() => { if (active) setError("beginFailed"); }).finally(() => { if (active) setBusy(false); });
      }
    }).catch(() => { if (active) setError("serviceUnavailable"); });
    return () => { active = false; };
  }, [client, providerTest]);

  useEffect(() => {
    const resendAt = Date.parse(step?.resendAfter ?? "");
    if (!Number.isFinite(resendAt) || resendAt <= Date.now()) return;
    const timer = globalThis.setTimeout(() => setClock(Date.now()), Math.min(resendAt - Date.now() + 25, 2_147_483_647));
    return () => globalThis.clearTimeout(timer);
  }, [step?.resendAfter]);

  const copy = localizedMessages(locale);
  const styles = useMemo(() => accessAppearance(access?.accessTemplate, systemScheme), [access?.accessTemplate, systemScheme]);

  const begin = async (nextMethodID = methodId, replaceCurrent = false) => {
    if (nextMethodID === "") return;
    setBusy(true); setError(undefined);
    try {
      if (replaceCurrent && transactionId !== undefined) { try { await client.cancel(transactionId); } catch { /* expired transaction is already terminal */ } }
      setMethodId(nextMethodID); setTransactionId(undefined); setStep(undefined);
      const value = await beginAuthentication(client, providerTest, nextMethodID, locale);
      setTransactionId(value.transactionId); setStep(value.result.step);
    } catch { setError("beginFailed"); if (methods.length > 1) setMethodId(""); }
    finally { setBusy(false); }
  };
  const submit = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    if (transactionId === undefined || step === undefined) return;
    setBusy(true); setError(undefined);
    const form = new FormData(event.currentTarget);
    const responses = step.fields.map((field) => ({ fieldId: field.id, value: String(form.get(field.id) ?? "") }));
    event.currentTarget.reset();
    try {
      const value = await client.continue(transactionId, step.stepId, responses);
      if (value.result.state === "authenticated") { globalThis.location?.assign(value.returnTo ?? "/"); return; }
      setStep(value.result.step); setError(resultMessage(value.result.state, step.kind));
    } catch { setError("authenticationFailed"); }
    finally { setBusy(false); }
  };
  const resend = async () => {
    if (transactionId === undefined) return;
    setBusy(true); setError(undefined);
    try { const value = await client.resend(transactionId); setStep(value.result.step); }
    catch { setError("resendFailed"); }
    finally { setBusy(false); }
  };
  const product = access === undefined ? "VastPlan" : localized(access.branding.productName, locale, "VastPlan");
  const logo = access === undefined ? undefined : accessBrandAssetURL(access, returnTo);
  return <main style={styles.canvas} lang={locale} dir={accessLocaleDirection(locale)}>
    {access === undefined || access.localization.supportedLocales.length <= 1 ? null : <AccessLocaleSelector locale={locale} supportedLocales={access.localization.supportedLocales} label={copy.language} styles={styles} onChange={setLocale} />}
    <section aria-labelledby="access-title" style={styles.card}>
      <header style={styles.header}>{logo === undefined ? <div aria-hidden="true" style={styles.logo}>V</div> : <img src={logo} alt="" style={styles.logoImage} />}<strong>{product}</strong></header>
      <h1 id="access-title" style={styles.title}>{localized(step?.title, locale, copy.login)}</h1>
      <p style={styles.description}>{localized(step?.description, locale, copy.chooseMethod)}</p>
      {methods.length <= 1 ? null : <AccessMethodSelector methods={methods} selectedMethodID={methodId} locale={locale} label={copy.methods} placeholder={copy.chooseMethodPlaceholder} busy={busy} styles={styles} onChoose={(next) => void begin(next, true)} />}
      {error === undefined ? null : <p role="alert" style={styles.error}>{copy[error]}</p>}
      {step === undefined ? methods.length > 1 ? null : busy ? <p role="status" style={styles.connecting}>{copy.connecting}</p> : error === undefined ? null : <button type="button" disabled={methodId === ""} onClick={() => void begin()} style={styles.secondary}>{copy.retry}</button> : step.kind === "redirect" ? <button type="button" disabled={busy || step.redirectUri === undefined} onClick={() => step.redirectUri === undefined ? undefined : globalThis.location?.assign(step.redirectUri)} style={styles.primary}>{localized(step.submitLabel, locale, copy.enterpriseLogin)}</button> : <form onSubmit={(event) => void submit(event)} style={styles.form}>
        {step.fields.map((field) => <label key={field.id} style={styles.field}><span>{localized(field.label, locale, field.id)}</span>{field.kind === "select" ? <select name={field.id} required={field.required} style={styles.input}>{field.choices.map((choice) => <option key={choice.value} value={choice.value}>{localized(choice.label, locale, choice.value)}</option>)}</select> : <input name={field.id} type={field.kind === "password" ? "password" : "text"} autoComplete={field.autocomplete} required={field.required} minLength={field.minLength} maxLength={field.maxLength} inputMode={field.kind === "one-time-code" ? "numeric" : undefined} style={styles.input} />}<small style={styles.help}>{localized(field.help, locale, "")}</small></label>)}
        <button type="submit" disabled={busy} style={styles.primary}>{busy ? copy.verifying : localized(step.submitLabel, locale, copy.login)}</button>
        {step.resendAfter === undefined ? null : <div style={styles.actions}><button type="button" disabled={busy || Date.parse(step.resendAfter) > clock} onClick={() => void resend()} style={styles.secondary}>{copy.resend}</button></div>}
      </form>}
      <footer style={styles.footer}>{access?.branding.privacyPath === undefined ? null : <a href={access.branding.privacyPath}>{copy.privacy}</a>}{access?.branding.supportPath === undefined ? null : <a href={access.branding.supportPath}>{copy.help}</a>}</footer>
    </section>
  </main>;
}

function selectLocale(supported: readonly string[], fallback: string): string { for (const candidate of globalThis.navigator?.languages ?? []) { const match = supported.find((value) => value.toLowerCase() === candidate.toLowerCase() || value.split("-")[0].toLowerCase() === candidate.split("-")[0].toLowerCase()); if (match !== undefined) return match; } return fallback; }
function resultMessage(state: string, stepKind: AuthenticationStep["kind"]): MessageKey {
  if (stepKind === "enrollment") return state === "expired" ? "expired" : "initializationFailed";
  return state === "locked" ? "locked" : state === "expired" ? "expired" : "invalid";
}
function localizedMessages(locale: string): Readonly<Record<MessageKey, string>> { return locale.toLowerCase().startsWith("zh") ? messages["zh-CN"] : messages["en-US"]; }
function beginAuthentication(client: AccessAuthenticationClient, providerTest: ReturnType<typeof providerTestSelection>, methodID: string, locale: string) { return providerTest === undefined ? client.begin(methodID, locale) : client.beginProviderTest(providerTest.providerProfileId, providerTest.methodId, locale); }

const messages = {
  "zh-CN": { language: "语言", login: "登录", chooseMethod: "请选择企业提供的登录方式", chooseMethodPlaceholder: "选择登录方式", methods: "登录方式", connecting: "正在连接…", retry: "重试", enterpriseLogin: "前往企业登录", verifying: "正在验证…", resend: "重新发送", privacy: "隐私", help: "帮助", serviceUnavailable: "登录服务暂时不可用，请稍后重试。", beginFailed: "无法开始登录，请稍后重试。", authenticationFailed: "登录未完成，请重新尝试。", initializationFailed: "管理员设置未完成，请确认账号、密码和确认密码后重试。", resendFailed: "暂时无法重新发送，请稍后再试。", locked: "尝试次数过多，请稍后再试。", expired: "登录已过期，请重新开始。", invalid: "登录信息无效，请检查后重试。" },
  "en-US": { language: "Language", login: "Sign in", chooseMethod: "Choose a sign-in method provided by your organization", chooseMethodPlaceholder: "Choose a sign-in method", methods: "Sign-in methods", connecting: "Connecting…", retry: "Retry", enterpriseLogin: "Continue to enterprise sign-in", verifying: "Verifying…", resend: "Resend", privacy: "Privacy", help: "Help", serviceUnavailable: "The sign-in service is temporarily unavailable. Try again later.", beginFailed: "Unable to start sign-in. Try again later.", authenticationFailed: "Sign-in was not completed. Try again.", initializationFailed: "Administrator setup was not completed. Check the account and both password entries, then try again.", resendFailed: "Unable to resend right now. Try again later.", locked: "Too many attempts. Try again.", expired: "Your sign-in expired. Start again.", invalid: "The sign-in information is invalid. Check it and try again." },
} as const;
