const maximumBuckets = 100_000;

interface RateBucket {
  startedAt: number;
  count: number;
}

// 这是单 Gateway 实例的入口防护，不替代共享 Authorization/Quota Provider。
export class APIExposureRateLimiter {
  private readonly buckets = new Map<string, RateBucket>();

  public constructor(private readonly now: () => number = Date.now) {}

  public allow(routeKey: string, principalID: string, requestsPerMinute: number): boolean {
    const now = this.now();
    const key = `${routeKey}\0${principalID}`;
    const existing = this.buckets.get(key);
    if (existing !== undefined && now - existing.startedAt < 60_000) {
      if (existing.count >= requestsPerMinute) return false;
      existing.count += 1;
      return true;
    }
    if (this.buckets.size >= maximumBuckets) this.sweep(now);
    if (this.buckets.size >= maximumBuckets && !this.buckets.has(key)) return false;
    this.buckets.set(key, { startedAt: now, count: 1 });
    return true;
  }

  private sweep(now: number): void {
    for (const [key, bucket] of this.buckets) if (now - bucket.startedAt >= 60_000) this.buckets.delete(key);
  }
}
