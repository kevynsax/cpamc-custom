/**
 * Generic API call helper (proxied via management API).
 */

import type { AxiosRequestConfig } from 'axios';
import { apiClient } from './client';
import { quotaCacheRoute } from './customQuotaApiCall'; // CUSTOM (cpamc-custom)

// CUSTOM (cpamc-custom): quota-scoped calls use the backend cache endpoint; a stock
// backend without it (404) transparently falls back to the direct proxy.
const postApiCall = async (payload: ApiCallRequest, config?: AxiosRequestConfig) => {
  const route = quotaCacheRoute();
  if (route) {
    try {
      return await apiClient.post<Record<string, unknown>>(
        route.path,
        { ...payload, force: route.force },
        config
      );
    } catch (err: unknown) {
      if ((err as { status?: number })?.status !== 404) throw err;
    }
  }
  return apiClient.post<Record<string, unknown>>('/requests/api-call', payload, config);
};
import { isRecord } from '@/utils/helpers';

export interface ApiCallRequest {
  authIndex?: string;
  proxy_url?: string;
  method: string;
  url: string;
  header?: Record<string, string>;
  data?: string;
}

export interface ApiCallResult<T = unknown> {
  statusCode: number;
  header: Record<string, string[]>;
  bodyText: string;
  body: T | null;
}

const normalizeBody = (input: unknown): { bodyText: string; body: unknown | null } => {
  if (input === undefined || input === null) {
    return { bodyText: '', body: null };
  }

  if (typeof input === 'string') {
    const text = input;
    const trimmed = text.trim();
    if (!trimmed) {
      return { bodyText: text, body: null };
    }
    try {
      return { bodyText: text, body: JSON.parse(trimmed) };
    } catch {
      return { bodyText: text, body: text };
    }
  }

  try {
    return { bodyText: JSON.stringify(input), body: input };
  } catch {
    return { bodyText: String(input), body: input };
  }
};

export const getApiCallErrorMessage = (result: ApiCallResult): string => {
  const status = result.statusCode;
  const body = result.body;
  const bodyText = result.bodyText;
  let message = '';

  if (isRecord(body)) {
    const errorValue = body.error;
    if (isRecord(errorValue) && typeof errorValue.message === 'string') {
      message = errorValue.message;
    } else if (typeof errorValue === 'string') {
      message = errorValue;
    }
    if (!message && typeof body.message === 'string') {
      message = body.message;
    }
  } else if (typeof body === 'string') {
    message = body;
  }

  if (!message && bodyText) {
    message = bodyText;
  }

  if (status && message) return `${status} ${message}`.trim();
  if (status) return `HTTP ${status}`;
  return message || 'Request failed';
};

export const apiCallApi = {
  request: async (payload: ApiCallRequest, config?: AxiosRequestConfig): Promise<ApiCallResult> => {
    const response = await postApiCall(payload, config); // CUSTOM (cpamc-custom)
    const statusCode = Number(response?.status_code ?? 0);
    const header = (response?.header ?? {}) as Record<string, string[]>;
    const { bodyText, body } = normalizeBody(response?.body);

    return {
      statusCode,
      header,
      bodyText,
      body,
    };
  },
};
