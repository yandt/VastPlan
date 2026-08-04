type PortalLocation = Pick<Location, "pathname" | "search" | "hash" | "assign">;

/**
 * Portal 浏览器宿主统一处理登录后会话失效。
 *
 * 原响应保持不变，避免破坏调用方的错误处理；这里只触发顶层认证跳转，
 * 不自动重放可能携带一次性秘密或产生副作用的请求。
 */
export function createPortalSessionFetch(fetcher: typeof fetch, location: PortalLocation | undefined): typeof fetch {
  let redirecting = false;
  return async (input: RequestInfo | URL, init?: RequestInit): Promise<Response> => {
    const response = await fetcher(input, init);
    if (!redirecting && location !== undefined && location.pathname !== "/auth/access" && await isSessionRequired(response)) {
      const current = `${location.pathname}${location.search}${location.hash}`;
      const returnTo = validReturnTo(current) ? current : "/";
      redirecting = true;
      location.assign(`/auth/access?returnTo=${encodeURIComponent(returnTo)}`);
    }
    return response;
  };
}

async function isSessionRequired(response: Response): Promise<boolean> {
  if (response.status !== 401) return false;
  try {
    const value = await response.clone().json() as { error?: unknown };
    return value.error === "session_required";
  } catch {
    return false;
  }
}

function validReturnTo(value: string): boolean {
  return value.startsWith("/") && !value.startsWith("//") && value.length <= 2048 && !/[\u0000-\u001f\u007f\\]/.test(value);
}
