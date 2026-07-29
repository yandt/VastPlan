import { useEffect, useState } from "react";

export interface KernelRecoveryStatus {
  readonly overall: string;
  readonly scope: "local" | "cluster";
  readonly clusterAvailable: boolean;
  readonly nodes: number;
  readonly stages: readonly { readonly id: string; readonly status: string; readonly ready: number; readonly required: number }[];
}

export function useKernelRecoveryStatus(endpoint = "/v1/kernel-recovery"): { status?: KernelRecoveryStatus; unavailable: boolean } {
  const [status, setStatus] = useState<KernelRecoveryStatus>();
  const [unavailable, setUnavailable] = useState(false);
  useEffect(() => {
    const controller = new AbortController();
    const timeout = setTimeout(() => controller.abort(), 2_500);
    void fetch(endpoint, { method: "GET", cache: "no-store", signal: controller.signal })
      .then(async (response) => {
        if (!response.ok) throw new Error("kernel recovery unavailable");
        const value = await response.json() as KernelRecoveryStatus;
        if (!Array.isArray(value.stages) || typeof value.overall !== "string" || typeof value.nodes !== "number") throw new Error("kernel recovery invalid");
        setStatus(value);
      })
      .catch(() => setUnavailable(true))
      .finally(() => clearTimeout(timeout));
    return () => { clearTimeout(timeout); controller.abort(); };
  }, [endpoint]);
  return { ...(status === undefined ? {} : { status }), unavailable };
}
