import { useCallback, useEffect, useRef, useState } from 'react';
import {
  clientUsageApi,
  type ClientUsageResponse,
  type UsageSettings,
} from '@/services/api/clientUsage';
import { useAuthStore } from '@/stores';

export function useClientUsage() {
  const apiBase = useAuthStore((state) => state.apiBase);
  const managementKey = useAuthStore((state) => state.managementKey);
  const connected = useAuthStore((state) => state.connectionStatus === 'connected');
  const scope = JSON.stringify([apiBase, managementKey, connected]);
  const sequence = useRef(0);
  const alive = useRef(false);
  const mutating = useRef<string | null>(null);
  const [result, setResult] = useState<{ scope: string; data: ClientUsageResponse } | null>(null);
  const [status, setStatus] = useState<{ scope: string; loading: boolean; error: boolean } | null>(
    null
  );
  const [mutation, setMutation] = useState<{ scope: string; busy: boolean; error: boolean } | null>(
    null
  );
  const matches = useCallback(() => {
    const current = useAuthStore.getState();
    return (
      alive.current &&
      current.apiBase === apiBase &&
      current.managementKey === managementKey &&
      current.connectionStatus === 'connected'
    );
  }, [apiBase, managementKey]);
  const refresh = useCallback(async () => {
    if (!connected || !matches()) return;
    const request = ++sequence.current;
    setStatus({ scope, loading: true, error: false });
    try {
      const data = await clientUsageApi.get();
      if (matches() && request === sequence.current) {
        setResult({ scope, data });
        setStatus({ scope, loading: false, error: false });
      }
    } catch {
      if (matches() && request === sequence.current)
        setStatus({ scope, loading: false, error: true });
    }
  }, [connected, matches, scope]);
  const invalidate = useCallback(() => {
    alive.current = false;
    sequence.current++;
  }, []);
  useEffect(() => {
    alive.current = true;
    void refresh();
    return invalidate;
  }, [refresh, invalidate]);
  const mutate = async (action: () => Promise<unknown>) => {
    if (!matches() || mutating.current === scope) return;
    mutating.current = scope;
    setMutation({ scope, busy: true, error: false });
    try {
      await action();
      if (matches()) {
        await refresh();
        if (matches()) setMutation({ scope, busy: false, error: false });
      }
    } catch {
      if (matches()) setMutation({ scope, busy: false, error: true });
    } finally {
      if (mutating.current === scope) mutating.current = null;
    }
  };
  return {
    connected,
    refresh,
    data: result?.scope === scope ? result.data : null,
    loading: status?.scope === scope && status.loading,
    error:
      (status?.scope === scope && status.error) || (mutation?.scope === scope && mutation.error),
    saving: mutation?.scope === scope && mutation.busy,
    saveSettings: (settings: UsageSettings) => mutate(() => clientUsageApi.saveSettings(settings)),
    refreshPrices: () => mutate(() => clientUsageApi.refreshPrices()),
  };
}
