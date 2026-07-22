import { useEffect, useMemo, useState, type CSSProperties, type FormEvent } from "react";
import { AccessAuthenticationClient, accessReturnTo, providerTestSelection, type AccessBootstrap, type AuthenticationMethod, type AuthenticationStep } from "./access-authentication-client";
import type { ModuleFetcher } from "./module-loader";

type MessageKey = keyof typeof messages["zh-CN"];

export function AccessLoginPage({ fetcher }: { fetcher: ModuleFetcher }) {
  const client = useMemo(() => new AccessAuthenticationClient(fetcher, accessReturnTo(globalThis.location)), [fetcher]);
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

  useEffect(() => {
    let active = true;
    void (providerTest === undefined ? client.bootstrap() : client.bootstrapProviderTest(providerTest.methodId)).then((value) => {
      if (!active) return;
      const selectedLocale = selectLocale(value.access.localization.supportedLocales, value.access.localization.defaultLocale);
      const testMethod = providerTest === undefined ? undefined : value.methods.find(({ methodId }) => methodId === providerTest.methodId) ?? { methodId: providerTest.methodId, interaction: "form" as const, displayName: { "zh-CN": "Provider 认证测试" } };
      setAccess(value.access); setMethods(testMethod === undefined ? value.methods : [testMethod]); setMethodId(testMethod?.methodId ?? value.defaultMethod); setLocale(selectedLocale);
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

  const begin = async () => {
    setBusy(true); setError(undefined);
    try {
      const value = providerTest === undefined ? await client.begin(methodId, locale) : await client.beginProviderTest(providerTest.providerProfileId, providerTest.methodId, locale);
      setTransactionId(value.transactionId); setStep(value.result.step);
    } catch { setError("beginFailed"); }
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
      setStep(value.result.step); setError(resultMessage(value.result.state));
    } catch { setError("authenticationFailed"); }
    finally { setBusy(false); }
  };
  const switchMethod = async (next: string) => {
    if (transactionId !== undefined) { try { await client.cancel(transactionId); } catch { /* expired transaction is already terminal */ } }
    setMethodId(next); setTransactionId(undefined); setStep(undefined); setError(undefined);
  };
  const resend = async () => {
    if (transactionId === undefined) return;
    setBusy(true); setError(undefined);
    try { const value = await client.resend(transactionId); setStep(value.result.step); }
    catch { setError("resendFailed"); }
    finally { setBusy(false); }
  };
  const cancel = async () => {
    if (transactionId !== undefined) { try { await client.cancel(transactionId); } catch { /* terminal transactions need no further cleanup */ } }
    setTransactionId(undefined); setStep(undefined); setError(undefined);
  };

  const product = access === undefined ? "VastPlan" : localized(access.branding.productName, locale, "VastPlan");
  return <main style={styles.canvas}>
    <section aria-labelledby="access-title" style={styles.card}>
      <header style={styles.header}><div aria-hidden="true" style={styles.logo}>V</div><strong>{product}</strong>{access !== undefined && access.localization.supportedLocales.length > 1 ? <select aria-label={copy.language} value={locale} onChange={(event) => setLocale(event.currentTarget.value)} style={styles.locale}>{access.localization.supportedLocales.map((value) => <option key={value} value={value}>{value}</option>)}</select> : null}</header>
      <h1 id="access-title" style={styles.title}>{localized(step?.title, locale, copy.login)}</h1>
      <p style={styles.description}>{localized(step?.description, locale, copy.chooseMethod)}</p>
      {methods.length > 1 ? <nav aria-label={copy.methods} style={styles.methods}>{methods.map((method) => <button key={method.methodId} type="button" aria-pressed={method.methodId === methodId} disabled={busy} onClick={() => void switchMethod(method.methodId)} style={method.methodId === methodId ? styles.methodActive : styles.method}>{localized(method.displayName, locale, method.methodId)}</button>)}</nav> : null}
      {error === undefined ? null : <p role="alert" style={styles.error}>{copy[error]}</p>}
      {step === undefined ? <button type="button" disabled={busy || methodId === ""} onClick={() => void begin()} style={styles.primary}>{busy ? copy.connecting : copy.continue}</button> : step.kind === "redirect" ? <div style={styles.actions}><button type="button" disabled={busy || step.redirectUri === undefined} onClick={() => step.redirectUri === undefined ? undefined : globalThis.location?.assign(step.redirectUri)} style={styles.primary}>{localized(step.submitLabel, locale, copy.enterpriseLogin)}</button><button type="button" disabled={busy} onClick={() => void cancel()} style={styles.secondary}>{copy.cancel}</button></div> : <form onSubmit={(event) => void submit(event)} style={styles.form}>
        {step.fields.map((field) => <label key={field.id} style={styles.field}><span>{localized(field.label, locale, field.id)}</span>{field.kind === "select" ? <select name={field.id} required={field.required} style={styles.input}>{field.choices.map((choice) => <option key={choice.value} value={choice.value}>{localized(choice.label, locale, choice.value)}</option>)}</select> : <input name={field.id} type={field.kind === "password" ? "password" : "text"} autoComplete={field.autocomplete} required={field.required} minLength={field.minLength} maxLength={field.maxLength} inputMode={field.kind === "one-time-code" ? "numeric" : undefined} style={styles.input} />}<small style={styles.help}>{localized(field.help, locale, "")}</small></label>)}
        <button type="submit" disabled={busy} style={styles.primary}>{busy ? copy.verifying : localized(step.submitLabel, locale, copy.login)}</button>
        <div style={styles.actions}>{step.resendAfter === undefined ? null : <button type="button" disabled={busy || Date.parse(step.resendAfter) > clock} onClick={() => void resend()} style={styles.secondary}>{copy.resend}</button>}<button type="button" disabled={busy} onClick={() => void cancel()} style={styles.secondary}>{copy.cancel}</button></div>
      </form>}
      <footer style={styles.footer}>{access?.branding.privacyPath === undefined ? null : <a href={access.branding.privacyPath}>{copy.privacy}</a>}{access?.branding.supportPath === undefined ? null : <a href={access.branding.supportPath}>{copy.help}</a>}</footer>
    </section>
  </main>;
}

function localized(value: Readonly<Record<string, string>> | undefined, locale: string, fallback: string): string { return value?.[locale] ?? value?.[locale.split("-")[0]] ?? value?.[Object.keys(value)[0] ?? ""] ?? fallback; }
function selectLocale(supported: readonly string[], fallback: string): string { for (const candidate of globalThis.navigator?.languages ?? []) { const match = supported.find((value) => value.toLowerCase() === candidate.toLowerCase() || value.split("-")[0].toLowerCase() === candidate.split("-")[0].toLowerCase()); if (match !== undefined) return match; } return fallback; }
function resultMessage(state: string): MessageKey { return state === "locked" ? "locked" : state === "expired" ? "expired" : "invalid"; }
function localizedMessages(locale: string): Readonly<Record<MessageKey, string>> { return locale.toLowerCase().startsWith("zh") ? messages["zh-CN"] : messages["en-US"]; }

const messages = {
  "zh-CN": { language: "语言", login: "登录", chooseMethod: "请选择企业提供的登录方式", methods: "登录方式", connecting: "正在连接…", continue: "继续", enterpriseLogin: "前往企业登录", verifying: "正在验证…", resend: "重新发送", cancel: "取消", privacy: "隐私", help: "帮助", serviceUnavailable: "登录服务暂时不可用，请稍后重试。", beginFailed: "无法开始登录，请稍后重试。", authenticationFailed: "登录未完成，请重新尝试。", resendFailed: "暂时无法重新发送，请稍后再试。", locked: "尝试次数过多，请稍后再试。", expired: "登录已过期，请重新开始。", invalid: "登录信息无效，请检查后重试。" },
  "en-US": { language: "Language", login: "Sign in", chooseMethod: "Choose a sign-in method provided by your organization", methods: "Sign-in methods", connecting: "Connecting…", continue: "Continue", enterpriseLogin: "Continue to enterprise sign-in", verifying: "Verifying…", resend: "Resend", cancel: "Cancel", privacy: "Privacy", help: "Help", serviceUnavailable: "The sign-in service is temporarily unavailable. Try again later.", beginFailed: "Unable to start sign-in. Try again later.", authenticationFailed: "Sign-in was not completed. Try again.", resendFailed: "Unable to resend right now. Try again later.", locked: "Too many attempts. Try again later.", expired: "Your sign-in expired. Start again.", invalid: "The sign-in information is invalid. Check it and try again." },
} as const;

const styles: Record<string, CSSProperties> = {
  canvas: { minHeight: "100vh", display: "grid", placeItems: "center", padding: 24, boxSizing: "border-box", background: "#f7f8fa", color: "#1d2129", fontFamily: "Inter, ui-sans-serif, system-ui, sans-serif" },
  card: { width: "min(420px, 100%)", boxSizing: "border-box", padding: "32px 36px", border: "1px solid #e5e6eb", borderRadius: 12, background: "#fff", boxShadow: "0 12px 36px rgba(23, 43, 77, .08)" },
  header: { height: 40, display: "flex", alignItems: "center", gap: 10, marginBottom: 28 }, logo: { width: 32, height: 32, display: "grid", placeItems: "center", borderRadius: 8, background: "#165dff", color: "#fff", fontWeight: 700 }, locale: { marginLeft: "auto", minHeight: 32, border: "1px solid #c9cdd4", borderRadius: 6, background: "#fff", color: "inherit" },
  title: { margin: 0, fontSize: 24, lineHeight: 1.4 }, description: { margin: "8px 0 24px", color: "#86909c", lineHeight: 1.6 },
  methods: { display: "flex", gap: 8, marginBottom: 20, overflowX: "auto" }, method: { padding: "8px 12px", border: "1px solid #e5e6eb", borderRadius: 6, background: "#fff", cursor: "pointer" }, methodActive: { padding: "8px 12px", border: "1px solid #165dff", borderRadius: 6, background: "#e8f3ff", color: "#165dff", cursor: "pointer" },
  form: { display: "grid", gap: 16 }, field: { display: "grid", gap: 7, fontSize: 14 }, input: { width: "100%", minHeight: 40, boxSizing: "border-box", padding: "8px 12px", border: "1px solid #c9cdd4", borderRadius: 6, background: "#fff", color: "inherit", font: "inherit" }, help: { minHeight: 18, color: "#86909c" },
  primary: { width: "100%", minHeight: 42, border: 0, borderRadius: 6, background: "#165dff", color: "#fff", font: "inherit", cursor: "pointer" }, secondary: { minHeight: 40, padding: "0 16px", border: "1px solid #c9cdd4", borderRadius: 6, background: "#fff", font: "inherit", cursor: "pointer" }, actions: { display: "flex", gap: 10 },
  error: { padding: "10px 12px", borderRadius: 6, background: "#fff2f0", color: "#cb2634", fontSize: 14 }, footer: { minHeight: 24, display: "flex", justifyContent: "center", gap: 20, marginTop: 24, fontSize: 13 },
};
