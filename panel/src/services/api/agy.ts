// CUSTOM (cpamc-custom)
import { apiClient } from './client';
import { guardConfigConnection, isMissingConfigValue } from './configValue';
import type { GoogleAccountClient } from '@/types/visualConfig';

export type AgyAccount = {
  id: string;
  email: string;
  status: 'ready' | 'disabled' | 'refreshing';
};

type AgyAccountsResponse = { accounts: AgyAccount[] };
type SavedGoogleAccountClient = { name: string; 'api-key': string };
type AgyBootstrap = {
  access_token: string;
  refresh_token: string;
  token_type: string;
  expires_in: number;
  expiry: string;
  id_token_email: string;
};

export class AgySetupSaveRequiredError extends Error {}

async function validateSavedClient(
  connection: ReturnType<typeof apiClient.captureConnection>,
  client: GoogleAccountClient
) {
  const requests = [
    connection.get<SavedGoogleAccountClient[]>('/config/access/google-account-clients'),
    connection.get<string[]>('/config/access/api-keys'),
  ] as const;
  let clients: SavedGoogleAccountClient[];
  let keys: string[];
  try {
    [clients, keys] = await Promise.all(requests);
  } catch (failure) {
    if (isMissingConfigValue(failure)) throw new AgySetupSaveRequiredError();
    throw failure;
  }
  const matchesClient = (entry: SavedGoogleAccountClient) =>
    entry.name === client.name && entry['api-key'] === client.apiKey;
  if (!clients.some(matchesClient) || !keys.includes(client.apiKey)) {
    throw new AgySetupSaveRequiredError();
  }
}

export const agyApi = {
  async accounts(client: GoogleAccountClient): Promise<AgyAccount[]> {
    const assertConnection = guardConfigConnection();
    const connection = apiClient.captureConnection();
    await validateSavedClient(connection, client);
    assertConnection();
    const response = await connection.get<AgyAccountsResponse>('/access/agy/accounts');
    assertConnection();
    return response.accounts;
  },

  async bootstrap(client: GoogleAccountClient, accountId: string): Promise<Blob> {
    const assertConnection = guardConfigConnection();
    const connection = apiClient.captureConnection();
    await validateSavedClient(connection, client);
    assertConnection();
    const body = { client: client.name, account_id: accountId };
    const token = await connection.post<AgyBootstrap>('/access/agy/bootstrap', body);
    assertConnection();
    const json = JSON.stringify(token, null, 2);
    const parts = [json];
    const options = { type: 'application/json' };
    const blob = new Blob(parts, options);
    return blob;
  },
};
