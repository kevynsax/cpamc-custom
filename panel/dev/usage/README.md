# Local usage preview

```bash
cd panel
bun run dev:usage
```

Open <http://127.0.0.1:5190/#/usage>. React and SCSS edits update through Vite HMR.
No gateway login, provider credentials, or inference requests are needed.

The eight-account lineup mirrors the server's quota view captured on 2026-10-17:
Antigravity, Claude Max 20×, Codex Pro 100, Meta, MiniMax, two Grok Pro accounts,
and Z.ai. Identities and usage are synthetic. The page merges these into seven
company donut charts below a per-person spending card, with current-week and calendar-month views, normalized/averaged quota
families, plan weighting, prorated crossing cycles, and distinct available/wasted
capacity. Incomplete company pies are hidden instead of rendering unknown or
unattributed slices. Per-account progress uses synthetic `@example.test` emails.
Money tags use prorated subscription fees, separate already-wasted quota from
currently available quota, and allocate known values by each person's usage share.
Unknown fees remain unset. Hover or focus reveals names, percentages and BRL amounts;
arrow keys navigate slices, click/tap pins a slice, and Escape closes its tooltip. Synthetic session windows exercise plans with multiple limits.

API-key names use the existing configuration key-name store. There is no second
people-name editor. Model-price cards use the 68-model inventory and researched
public references in `internal/usagehistory/model_reference.json`, including native
image/second/character units. Missing tariffs remain blank, not zero. Reference-only
media rates are not treated as metered token charges.

The one intentional live connection is the anonymous public Cross USDT/BRL buy
quote at `wss://cross-otc.com/api/v1/websocket`. No keys or usage are sent. Quotes
expire after 45 seconds; BRL fields pin their conversion quote while being edited,
then save canonical USD rates. All other usage endpoints are local fake responses,
never proxies to production or inference providers. Price edits last until restart.

Edit `fixtures.ts` to change usage scenarios. The preview entry and fake endpoints
are enabled only by `vite --mode usage-demo` while serving. Production builds keep
the real entry point and authentication, with no demo bootstrap. The dev server
binds loopback; its browser connection policy permits only local APIs/HMR and the
explicit Cross quote source. Filesystem access outside the panel is limited to the
single non-secret researched JSON file, not the gateway's auth/config directories.
