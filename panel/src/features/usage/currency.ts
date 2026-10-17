export interface CrossQuote {
  rate: number;
  observedAt: number;
}
export const CROSS_QUOTE_URL = 'wss://cross-otc.com/api/v1/websocket';
export const CROSS_QUOTE_MAX_AGE = 45_000;

export function parseCrossQuote(raw: string, now = Date.now()): CrossQuote | null {
  try {
    const data = JSON.parse(raw);
    if (data?.event !== 'update_prices' || data?.payload?.symbol !== 'usdt-brl') return null;
    const value = data.payload.prices?.buy;
    const rate =
      typeof value === 'number'
        ? value
        : typeof value === 'string' && value.trim()
          ? Number(value)
          : NaN;
    return Number.isFinite(rate) && rate > 0 ? { rate, observedAt: now } : null;
  } catch {
    return null;
  }
}

export function formatReais(usd: number, quote: CrossQuote | null, now = Date.now()): string {
  if (!quote || now - quote.observedAt > CROSS_QUOTE_MAX_AGE) return '—';
  const value = usd * quote.rate;
  if (!Number.isFinite(value)) return '—';
  const formatter = new Intl.NumberFormat('pt-BR', {
    style: 'currency',
    currency: 'BRL',
    minimumFractionDigits: 2,
    maximumFractionDigits: value > 0 && value < 0.01 ? 6 : 2,
  });
  return value > 0 && value < 0.000001
    ? `< ${formatter.format(0.000001)}`
    : formatter.format(value);
}
