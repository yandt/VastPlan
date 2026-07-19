/** Re-targets Arco's document selectors to the Portal Shadow DOM host. */
export function scopeDocumentCSS(css: string): string {
  return css
    .replace(/(^|[{},])(\s*)html\s*,\s*body(?=\s*\{)/g, "$1$2:host")
    .replace(/(^|[{},])(\s*)body(\[[^\]{}]+\])(?=\s*(?:[.{:#>+~]|\{))/g, "$1$2:host($3)")
    .replace(/(^|[{},])(\s*)body(?=\s*(?:[.{:#>+~]|\{))/g, "$1$2:host")
    .replace(/(^|[{}])(\s*)\*(?=\s*\{)/g, "$1$2:host, :host *");
}
