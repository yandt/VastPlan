import type { ModuleFetcher } from "./module-loader";

/** Performs the complete CSRF-protected logout handshake owned by the Portal Host. */
export async function logoutPortalSession(fetcher: ModuleFetcher): Promise<void> {
  const csrfResponse = await fetcher("/auth/v1/csrf", { credentials: "same-origin", cache: "no-store" });
  const csrf = await parseCSRF(csrfResponse);
  if (csrf === undefined) throw new PortalLogoutError("退出登录前获取 CSRF 令牌失败 (" + csrfResponse.status + ")");
  const response = await fetcher("/auth/logout", {
    method: "POST",
    credentials: "same-origin",
    cache: "no-store",
    headers: { "X-VastPlan-CSRF": csrf },
  });
  if (response.status !== 204) throw new PortalLogoutError("退出登录失败 (" + response.status + ")");
}

/** Keeps the post-logout destination local to the current Portal origin. */
export function portalLogoutRedirect(pathname: string): string {
  const returnTo = pathname.startsWith("/") && !pathname.startsWith("//") && pathname.length <= 2048 && !/[\0\r\n\\]/.test(pathname) ? pathname : "/";
  return "/auth/login?returnTo=" + encodeURIComponent(returnTo);
}

export class PortalLogoutError extends Error {
  public constructor(message: string) { super(message); this.name = "PortalLogoutError"; }
}

async function parseCSRF(response: Response): Promise<string | undefined> {
  try {
    const value = await response.json() as { token?: unknown };
    return response.ok && typeof value.token === "string" && value.token.length >= 32 ? value.token : undefined;
  } catch {
    return undefined;
  }
}
