import { useEffect, useState } from 'react';
import { CROSS_QUOTE_MAX_AGE, CROSS_QUOTE_URL, parseCrossQuote, type CrossQuote } from './currency';

export function useCrossQuote() {
  const [quote, setQuote] = useState<CrossQuote | null>(null);
  useEffect(() => {
    let active = true;
    let socket: WebSocket | null = null;
    let retry: ReturnType<typeof setTimeout> | undefined;
    let ping: ReturnType<typeof setInterval> | undefined;
    let delay = 2000;
    const connect = () => {
      if (!active) return;
      try {
        socket = new WebSocket(CROSS_QUOTE_URL);
      } catch {
        retry = setTimeout(connect, delay);
        return;
      }
      socket.onopen = () => {
        if (!active) return;
        delay = 2000;
        socket?.send(JSON.stringify({ subscribe: 'tickers' }));
        ping = setInterval(() => {
          if (socket?.readyState === WebSocket.OPEN) socket.send(JSON.stringify({ get: 'ping' }));
        }, 30_000);
      };
      socket.onmessage = (event) => {
        const next = typeof event.data === 'string' ? parseCrossQuote(event.data) : null;
        if (active && next) setQuote(next);
      };
      socket.onerror = () => {
        /* onclose owns reconnect; never create parallel subscriptions. */
      };
      socket.onclose = () => {
        clearInterval(ping);
        if (active) {
          retry = setTimeout(connect, delay);
          delay = Math.min(delay * 2, 30_000);
        }
      };
    };
    connect();
    const freshness = setInterval(
      () =>
        setQuote((current) =>
          current && Date.now() - current.observedAt > CROSS_QUOTE_MAX_AGE ? null : current
        ),
      1000
    );
    return () => {
      active = false;
      clearTimeout(retry);
      clearInterval(ping);
      clearInterval(freshness);
      socket?.close();
    };
  }, []);
  return quote;
}
