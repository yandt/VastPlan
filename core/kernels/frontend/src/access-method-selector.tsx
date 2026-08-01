import type { CSSProperties } from "react";
import type { AuthenticationMethod } from "./access-authentication-client";
import { localizeAccessText } from "./access-authentication-client";

export type AccessMethodSelectorStyles = Readonly<{
  methods: CSSProperties;
  method: CSSProperties;
  methodActive: CSSProperties;
  methodSelect: CSSProperties;
  methodSelectLabel: CSSProperties;
  methodSelectInput: CSSProperties;
}>;

export function AccessMethodSelector({ methods, selectedMethodID, locale, label, placeholder, busy, styles, onChoose }: {
  methods: readonly AuthenticationMethod[];
  selectedMethodID: string;
  locale: string;
  label: string;
  placeholder: string;
  busy: boolean;
  styles: AccessMethodSelectorStyles;
  onChoose(methodID: string): void;
}) {
  if (accessMethodPresentation(methods.length) === "select") {
    return <label style={styles.methodSelect}>
      <span style={styles.methodSelectLabel}>{label}</span>
      <select aria-label={label} value={selectedMethodID} disabled={busy} onChange={(event) => onChoose(event.currentTarget.value)} style={styles.methodSelectInput}>
        <option value="" disabled>{placeholder}</option>
        {methods.map((method) => <option key={method.methodId} value={method.methodId}>{localizeAccessText(method.displayName, locale, method.methodId)}</option>)}
      </select>
    </label>;
  }
  return <nav aria-label={label} role="tablist" style={styles.methods}>
    {methods.map((method) => <button key={method.methodId} type="button" role="tab" aria-selected={method.methodId === selectedMethodID} disabled={busy} onClick={() => onChoose(method.methodId)} style={method.methodId === selectedMethodID ? styles.methodActive : styles.method}>{localizeAccessText(method.displayName, locale, method.methodId)}</button>)}
  </nav>;
}

export function accessMethodPresentation(count: number): "tabs" | "select" {
  return count > 3 ? "select" : "tabs";
}
