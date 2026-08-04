/**
 * 浏览器内存中的 CSRF 双提交令牌会话。
 *
 * 令牌绝不写入 localStorage/sessionStorage；刷新页面或关闭页签后由宿主重新签发。
 * 成功的受保护写请求会刷新闲置期限，与 Portal Host 的 Cookie 续期保持一致。
 */
const idleLifetimeMilliseconds = 15 * 60 * 1_000;

export class CSRFTokenCache {
  private token: string | undefined;
  private idleExpiresAt = 0;
  private issuing: Promise<string> | undefined;

  public constructor(private readonly issue: () => Promise<string>) {}

  public async current(): Promise<string> {
    if (this.token !== undefined && this.idleExpiresAt > Date.now()) return this.token;
    if (this.issuing !== undefined) return this.issuing;
    const issuing = this.issue().then((token) => {
      this.token = token;
      this.touch();
      return token;
    });
    this.issuing = issuing;
    try {
      return await issuing;
    } finally {
      if (this.issuing === issuing) this.issuing = undefined;
    }
  }

  public touch(): void {
    if (this.token !== undefined) this.idleExpiresAt = Date.now() + idleLifetimeMilliseconds;
  }

  public clear(): void {
    this.token = undefined;
    this.idleExpiresAt = 0;
  }
}

/** Retries exactly once only when the Portal Host rejected the request before business routing. */
export async function withCSRF<T>(cache: CSRFTokenCache, invoke: (token: string) => Promise<T>, rejected: (error: unknown) => boolean): Promise<T> {
  let retried = false;
  for (;;) {
    const token = await cache.current();
    try {
      const result = await invoke(token);
      cache.touch();
      return result;
    } catch (error) {
      if (retried || !rejected(error)) {
        cache.touch();
        throw error;
      }
      retried = true;
      cache.clear();
    }
  }
}
