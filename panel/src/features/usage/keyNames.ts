import { sha256 } from '@noble/hashes/sha2.js';
import { bytesToHex } from '@noble/hashes/utils.js';
import { apiKeyNameFingerprint, readApiKeyNames } from '@/features/config/apiKeyNames';

// Usage hashes the key alone; the existing key editor scopes its names by connection.
export function resolveUsageKeyNames(
  apiBase: string,
  apiKeys: string[],
  stored = readApiKeyNames(apiBase)
): Record<string, string> {
  return Object.fromEntries(
    apiKeys.flatMap((key) => {
      const name = stored[apiKeyNameFingerprint(apiBase, key)];
      return name ? [[bytesToHex(sha256(new TextEncoder().encode(key))), name]] : [];
    })
  );
}
