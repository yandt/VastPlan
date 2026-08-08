import type { ShellBranding } from "./shell.js";

/** Shared Portal identity markup; each Shell Library owns its surrounding geometry and CSS. */
export function PortalBrand({ name, shortName, logoURL, className, markClassName, logoClassName, fullName = false, focusable = false }: ShellBranding & {
  className: string;
  markClassName: string;
  logoClassName: string;
  /** Compact desktop chrome expands to the complete Portal name. */
  fullName?: boolean;
  /** Lets an expandable identity reveal itself through keyboard focus as well as hover. */
  focusable?: boolean;
}) {
  const label = shortName ?? name;
  return <div className={className} tabIndex={focusable ? 0 : undefined}>{logoURL === undefined ? <span className={markClassName}>{label.slice(0, 1).toUpperCase()}</span> : <img src={logoURL} alt="" className={logoClassName} />}<strong>{fullName ? name : label}</strong></div>;
}
