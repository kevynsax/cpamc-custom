// CUSTOM (cpamc-custom): quota reads go through the backend cache endpoint.
import { afterEach, describe, expect, spyOn, test } from 'bun:test';
import { apiCallApi } from '@/services/api/apiCall';
import { apiClient } from '@/services/api/client';
import {
  QUOTA_CACHE_API_CALL_PATH,
  withQuotaCache,
  withQuotaForceRefresh,
} from '@/services/api/customQuotaApiCall';

const ok = { status_code: 200, header: {}, body: '{"ok":true}' };
const payload = { authIndex: '7', method: 'GET', url: 'https://example.test/usage' };

describe('custom backend quota cache routing', () => {
  let post: ReturnType<typeof spyOn> | undefined;
  afterEach(() => post?.mockRestore());

  test('plain calls keep using the upstream direct proxy', async () => {
    post = spyOn(apiClient, 'post').mockResolvedValue(ok as never);
    await apiCallApi.request(payload);
    expect(post.mock.calls[0][0]).toBe('/requests/api-call');
  });

  test('quota-scoped calls hit the cache endpoint without force', async () => {
    post = spyOn(apiClient, 'post').mockResolvedValue(ok as never);
    const result = await withQuotaCache(() => apiCallApi.request(payload));
    expect(post.mock.calls[0][0]).toBe(QUOTA_CACHE_API_CALL_PATH);
    expect(post.mock.calls[0][1]).toMatchObject({ ...payload, force: false });
    expect(result.body).toEqual({ ok: true });
  });

  test('explicit refresh forces a live backend call', async () => {
    post = spyOn(apiClient, 'post').mockResolvedValue(ok as never);
    await withQuotaForceRefresh(() => withQuotaCache(() => apiCallApi.request(payload)));
    expect(post.mock.calls[0][1]).toMatchObject({ force: true });
  });

  test('falls back to the direct proxy when the backend lacks the endpoint', async () => {
    post = spyOn(apiClient, 'post').mockImplementation((async (path: string) => {
      if (path === QUOTA_CACHE_API_CALL_PATH) throw Object.assign(new Error('nf'), { status: 404 });
      return ok;
    }) as never);
    const result = await withQuotaCache(() => apiCallApi.request(payload));
    expect(post.mock.calls.map((call) => call[0])).toEqual([
      QUOTA_CACHE_API_CALL_PATH,
      '/requests/api-call',
    ]);
    expect(result.statusCode).toBe(200);
  });

  test('other backend errors are surfaced, not retried', async () => {
    post = spyOn(apiClient, 'post').mockRejectedValue(Object.assign(new Error('boom'), { status: 502 }));
    await expect(withQuotaCache(() => apiCallApi.request(payload))).rejects.toThrow('boom');
    expect(post.mock.calls.length).toBe(1);
  });
});
