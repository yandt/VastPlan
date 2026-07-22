import { useCallback, useEffect, useRef, useState } from "react";

export interface RecordLoaderState {
  record?: Readonly<Record<string, unknown>>;
  loading: boolean;
  failure?: string;
  refresh(): void;
}

export function useRecordLoader(key: string | undefined, load: ((key: string, signal: AbortSignal) => Promise<Readonly<Record<string, unknown>> | undefined>) | undefined): RecordLoaderState {
  const [record, setRecord] = useState<Readonly<Record<string, unknown>>>();
  const [loading, setLoading] = useState(false);
  const [failure, setFailure] = useState<string>();
  const [revision, setRevision] = useState(0);
  const requestRef = useRef<AbortController>();
  useEffect(() => {
    requestRef.current?.abort();
    if (key === undefined || load === undefined) { setRecord(undefined); setLoading(false); setFailure(undefined); return; }
    const controller = new AbortController();
    requestRef.current = controller;
    setLoading(true); setFailure(undefined);
    void load(key, controller.signal).then((next) => {
      if (!controller.signal.aborted) setRecord(next);
    }).catch((error: unknown) => {
      if (!controller.signal.aborted) { setRecord(undefined); setFailure(error instanceof Error ? error.message : String(error)); }
    }).finally(() => { if (!controller.signal.aborted) setLoading(false); });
    return () => controller.abort();
  }, [key, load, revision]);
  return { record, loading, failure, refresh: useCallback(() => setRevision((value) => value + 1), []) };
}
