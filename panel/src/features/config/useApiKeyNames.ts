import { useCallback, useEffect, useRef, useState } from 'react';
import {
  keyNamesApi,
  type KeyMetadata,
  type ApiKeyKind,
  type ApiKeySpeed,
} from '@/services/api/keyNames';
import { apiClient } from '@/services/api/client';
import { useAuthStore } from '@/stores';
import { resolveUsageKeyNames } from '@/features/usage/keyNames';
import { clientKeyFingerprint, saveApiKeyName } from './apiKeyNames';

/** Server metadata is authoritative; browser names are imported only when absent. */
export function useApiKeyNames(apiKeys: string[]) {
  const apiBase = useAuthStore((state) => state.apiBase);
  const managementKey = useAuthStore((state) => state.managementKey);
  const connected = useAuthStore((state) => state.connectionStatus === 'connected');
  const scope = JSON.stringify([apiBase, managementKey, connected]);
  const keys = JSON.stringify(apiKeys);
  const generation = useRef(0);
  const invalidate = useCallback(() => {
    generation.current++;
  }, []);
  const [result, setResult] = useState<{ scope: string; metadata: KeyMetadata } | null>(null);
  const [status, setStatus] = useState<{ scope: string; error: boolean; busy: boolean } | null>(
    null
  );
  const sessionMatches = useCallback(() => {
    const current = useAuthStore.getState();
    return (
      connected &&
      current.apiBase === apiBase &&
      current.managementKey === managementKey &&
      current.connectionStatus === 'connected'
    );
  }, [apiBase, managementKey, connected]);
  useEffect(() => {
    if (!sessionMatches()) return;
    const request = ++generation.current;
    const revision = apiClient.getConnectionRevision();
    let alive = true;
    const matches = () =>
      alive &&
      sessionMatches() &&
      request === generation.current &&
      revision === apiClient.getConnectionRevision();
    setStatus({ scope, error: false, busy: true });
    void (async () => {
      try {
        let metadata = await keyNamesApi.get();
        if (!matches()) return;
        const local = resolveUsageKeyNames(apiBase, JSON.parse(keys) as string[]);
        const missing = Object.fromEntries(
          Object.entries(local).filter(
            ([id]) => !Object.prototype.hasOwnProperty.call(metadata.names, id)
          )
        );
        if (Object.keys(missing).length) {
          metadata = await keyNamesApi.patch(missing, true);
          if (!matches()) return;
        }
        setResult({ scope, metadata });
        setStatus({ scope, error: false, busy: false });
      } catch {
        if (matches()) setStatus({ scope, error: true, busy: false });
      }
    })();
    return () => {
      alive = false;
      invalidate();
    };
  }, [scope, keys, apiBase, sessionMatches, invalidate]);
  const save = async (apiKey: string, name: string, kind: ApiKeyKind, speed: ApiKeySpeed) => {
    if (!sessionMatches() || (status?.scope === scope && status.busy)) return false;
    const revision = apiClient.getConnectionRevision();
    const request = ++generation.current;
    const matches = () =>
      sessionMatches() &&
      revision === apiClient.getConnectionRevision() &&
      request === generation.current;
    setStatus({ scope, error: false, busy: true });
    try {
      const id = clientKeyFingerprint(apiKey);
      const metadata = await keyNamesApi.patch(
        { [id]: name.trim() },
        false,
        { [id]: kind },
        {
          [id]: speed,
        }
      );
      if (!matches()) return false;
      setResult({ scope, metadata });
      setStatus({ scope, error: false, busy: false });
      saveApiKeyName(apiBase, apiKey, name);
      return true;
    } catch {
      if (matches()) setStatus({ scope, error: true, busy: false });
      return false;
    }
  };
  return {
    names: result?.scope === scope ? result.metadata.names : {},
    kinds: result?.scope === scope ? result.metadata.kinds : {},
    speeds: result?.scope === scope ? result.metadata.speeds : {},
    busy: status?.scope === scope && status.busy,
    error: status?.scope === scope && status.error,
    save,
  };
}
