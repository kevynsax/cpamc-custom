import { useCallback, useEffect, useLayoutEffect, useRef, useState } from 'react';
import {
  clientUsageApi,
  type ClientUsageResponse,
  type UsageSettings,
} from '@/services/api/clientUsage';
import { useAuthStore } from '@/stores';

export function useClientUsage(from?: string, to?: string) {
  const apiBase = useAuthStore((state) => state.apiBase);
  const managementKey = useAuthStore((state) => state.managementKey);
  const connected = useAuthStore((state) => state.connectionStatus === 'connected');
  const connectionScope = JSON.stringify([apiBase, managementKey, connected]);
  const scope = JSON.stringify([connectionScope, from, to]);
  const sequence = useRef(0),
    alive = useRef(false),
    mutating = useRef<string | null>(null);
  const activeScope = useRef(scope);
  useLayoutEffect(() => {
    activeScope.current = scope;
  }, [scope]);
  const [result, setResult] = useState<{
    scope: string;
    connectionScope: string;
    data: ClientUsageResponse;
  } | null>(null);
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
      activeScope.current === scope &&
      current.apiBase === apiBase &&
      current.managementKey === managementKey &&
      current.connectionStatus === 'connected'
    );
  }, [apiBase, managementKey, scope]);
  const reload = useCallback(async () => {
    if (!connected || !matches()) return false;
    const request = ++sequence.current;
    setStatus({ scope, loading: true, error: false });
    try {
      const data = await clientUsageApi.get(from, to);
      if (!matches() || request !== sequence.current) return false;
      setResult({ scope, connectionScope, data });
      setStatus({ scope, loading: false, error: false });
      return true;
    } catch {
      if (matches() && request === sequence.current)
        setStatus({ scope, loading: false, error: true });
      return false;
    }
  }, [connected, matches, scope, connectionScope, from, to]);
  const refresh = useCallback(async () => {
    await reload();
  }, [reload]);
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
    if (!matches() || mutating.current === scope) return false;
    mutating.current = scope;
    setMutation({ scope, busy: true, error: false });
    try {
      await action();
      const success = matches() && (await reload());
      if (matches()) setMutation({ scope, busy: false, error: !success });
      return success;
    } catch {
      if (matches()) setMutation({ scope, busy: false, error: true });
      return false;
    } finally {
      if (mutating.current === scope) mutating.current = null;
    }
  };
  return {
    connected,
    refresh,
    data: result?.scope === scope ? result.data : null,
    catalog: result?.connectionScope === connectionScope ? result.data : null,
    loading: status?.scope === scope && status.loading,
    error:
      (status?.scope === scope && status.error) || (mutation?.scope === scope && mutation.error),
    saving: mutation?.scope === scope && mutation.busy,
    saveSettings: (settings: UsageSettings) => mutate(() => clientUsageApi.saveSettings(settings)),
  };
}
