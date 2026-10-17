import type { Plugin } from 'vite';
import type { IncomingMessage } from 'node:http';
import { createUsageFixture, type DemoSettings } from './fixtures';
import { demoPeople } from './people';

export function validDemoSettings(value: unknown): value is DemoSettings {
  if (!value || typeof value !== 'object' || Array.isArray(value)) return false;
  const settings = value as Record<string, unknown>;
  for (const key of ['names', 'plans']) {
    const map = settings[key];
    if (
      !map ||
      typeof map !== 'object' ||
      Array.isArray(map) ||
      !Object.values(map).every((entry) => typeof entry === 'string')
    )
      return false;
  }
  if (!settings.prices || typeof settings.prices !== 'object' || Array.isArray(settings.prices))
    return false;
  const validPrices = Object.values(settings.prices).every((value) => {
    if (!value || typeof value !== 'object') return false;
    const rate = value as Record<string, unknown>;
    return (
      ['input', 'output'].every(
        (key) =>
          typeof rate[key] === 'number' && Number.isFinite(rate[key]) && (rate[key] as number) >= 0
      ) &&
      ['cache_read', 'cache_write'].every(
        (key) =>
          rate[key] === undefined ||
          (typeof rate[key] === 'number' &&
            Number.isFinite(rate[key]) &&
            (rate[key] as number) >= 0)
      )
    );
  });
  const units = settings.unit_prices ?? {};
  return (
    validPrices &&
    !!units &&
    typeof units === 'object' &&
    !Array.isArray(units) &&
    Object.values(units).every(
      (entry) =>
        entry &&
        typeof entry === 'object' &&
        !Array.isArray(entry) &&
        Object.values(entry).every(
          (value) => typeof value === 'number' && Number.isFinite(value) && value >= 0
        )
    )
  );
}

async function readBody(request: IncomingMessage) {
  let body = '';
  for await (const chunk of request) {
    body += chunk.toString();
    if (body.length > 1_048_576) throw new Error('Request too large');
  }
  return JSON.parse(body) as unknown;
}

export function usageDemoPlugin(): Plugin {
  return {
    name: 'local-usage-demo',
    apply: 'serve',
    transformIndexHtml(html) {
      return html.replace('/src/main.tsx', '/dev/usage/main.tsx');
    },
    configureServer(server) {
      const fixture = createUsageFixture();
      server.middlewares.use(async (request, response, next) => {
        const url = new URL(request.url ?? '/', 'http://localhost');
        const pathname = url.pathname;
        if (!pathname.startsWith('/v8/management/') && !pathname.startsWith('/v1/')) return next();
        response.setHeader('Content-Type', 'application/json');
        response.setHeader('X-CPA-VERSION', 'local-fake-data');
        response.setHeader('Cache-Control', 'no-store');
        const reply = (value: unknown, status = 200) => {
          response.statusCode = status;
          response.end(JSON.stringify(value));
        };
        try {
          if (pathname === '/v8/management/config' && request.method === 'GET')
            return reply({
              'config-version': 8,
              access: { 'api-keys': demoPeople.map((person) => person.apiKey) },
              demo_key_names: Object.fromEntries(
                demoPeople.map((person) => [person.apiKey, person.name])
              ),
            });
          if (
            pathname === '/v8/management/observability/usage/clients' &&
            request.method === 'GET'
          ) {
            const from = url.searchParams.get('from') ?? fixture.from,
              to = url.searchParams.get('to') ?? fixture.to;
            const models = fixture.models.map((model) => {
              const unit = fixture.settings.unit_prices?.[model.id];
              const rate = fixture.settings.prices[model.id];
              const prices: Record<string, number> = unit
                ? { ...unit }
                : rate
                  ? {
                      input: rate.input,
                      output: rate.output,
                      ...(rate.cache_read === undefined ? {} : { cache_read: rate.cache_read }),
                      ...(rate.cache_write === undefined ? {} : { cache_write: rate.cache_write }),
                    }
                  : ({ ...model.prices } as Record<string, number>);
              return { ...model, prices, overridden: unit !== undefined || rate !== undefined };
            });
            return reply({
              ...fixture,
              from,
              to,
              models,
              rows: fixture.rows.filter((row) => row.date >= from && row.date <= to),
              weeks: fixture.weeks.filter(
                (week) => week.reset.slice(0, 10) >= from && week.start.slice(0, 10) <= to
              ),
            });
          }
          if (
            pathname === '/v8/management/observability/usage/settings' &&
            request.method === 'PUT'
          ) {
            const settings = await readBody(request);
            if (!validDemoSettings(settings))
              return reply({ error: 'Invalid preview settings' }, 400);
            fixture.settings = structuredClone(settings);
            return reply(fixture.settings);
          }
          if (
            pathname === '/v8/management/observability/usage/prices/refresh' &&
            request.method === 'POST'
          )
            return reply({ prices_as_of: fixture.prices_as_of });
          return reply({ error: 'Only fake usage endpoints are enabled in this preview' }, 404);
        } catch {
          return reply({ error: 'Invalid preview request' }, 400);
        }
      });
    },
  };
}
