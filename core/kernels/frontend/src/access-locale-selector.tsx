import { useEffect, useRef, useState, type CSSProperties } from "react";

export type AccessLocaleSelectorStyles = Readonly<{
  localePicker: CSSProperties;
  localeTrigger: CSSProperties;
  localeGlyph: CSSProperties;
  localeMenu: CSSProperties;
  localeOption: CSSProperties;
  localeOptionActive: CSSProperties;
  localeOptionGlyph: CSSProperties;
}>;

export function AccessLocaleSelector({ locale, supportedLocales, label, styles, onChange }: {
  locale: string;
  supportedLocales: readonly string[];
  label: string;
  styles: AccessLocaleSelectorStyles;
  onChange(locale: string): void;
}) {
  const [open, setOpen] = useState(false);
  const root = useRef<HTMLDivElement>(null);
  const selected = accessLocaleOption(locale);

  useEffect(() => {
    const closeWhenOutside = (event: PointerEvent) => {
      const element = root.current;
      if (element !== null && !event.composedPath().includes(element)) setOpen(false);
    };
    const closeWithEscape = (event: KeyboardEvent) => {
      if (event.key === "Escape") setOpen(false);
    };
    globalThis.addEventListener("pointerdown", closeWhenOutside);
    globalThis.addEventListener("keydown", closeWithEscape);
    return () => {
      globalThis.removeEventListener("pointerdown", closeWhenOutside);
      globalThis.removeEventListener("keydown", closeWithEscape);
    };
  }, []);

  return <div ref={root} style={styles.localePicker}>
    <button type="button" aria-label={`${label}: ${selected.label}`} aria-haspopup="menu" aria-expanded={open} onClick={() => setOpen((value) => !value)} style={styles.localeTrigger}>
      <span aria-hidden="true" style={styles.localeGlyph}>{selected.glyph}</span>
    </button>
    {!open ? null : <div role="menu" aria-label={label} style={styles.localeMenu}>
      {supportedLocales.map((value) => {
        const option = accessLocaleOption(value);
        const active = value === locale;
        return <button key={value} type="button" role="menuitemradio" aria-checked={active} onClick={() => { onChange(value); setOpen(false); }} style={active ? styles.localeOptionActive : styles.localeOption}>
          <span aria-hidden="true" style={styles.localeOptionGlyph}>{option.glyph}</span>
          <span>{option.label}</span>
        </button>;
      })}
    </div>}
  </div>;
}

export function accessLocaleOption(locale: string): Readonly<{ glyph: string; label: string }> {
  return locale.toLowerCase().startsWith("zh") ? { glyph: "中", label: "中文" } : { glyph: "EN", label: "English" };
}
