/** Re-targets only document-root selectors; Shadow DOM scopes all others. */
export function scopeDocumentCSS(css: string): string {
  return css
    .replace(/(^|\n)html,\nbody \{/g, "$1:host {")
    .replace(/(^|\n)body \{/g, "$1:host {")
    .replace(/(^|\n)\* \{/g, "$1:host, :host * {");
}
