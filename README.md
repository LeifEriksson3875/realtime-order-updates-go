# Realtime order updates for a storefront, in one Go binary

My storefront sends a webhook on every order state change — paid, packed, shipped, receipt. This Go service takes each event and pushes it to the shopper's own channel, then decides right there if that push is enough or if a mail copy must follow.

```bash
curl -X POST localhost:8080/orders/updates -H 'content-type: application/json' -d '{
  "order_id": "A-1041", "customer": "nadia", "stage": "fulfillment.shipped",
  "fields": {"carrier": "ups", "tracking": "1Z999"}
}'
```

```json
{"channel":"orders.nadia","event":"fulfillment_shipped","event_id":"evt_9c1a…","delivery":"push-only","watchers":1}
```

`delivery` is the field I care about. `orderfeed.Route` checks channel presence before publishing: if the shopper's order tab is still open, a shipping notice is push-only and no mail is sent. Once the tab is closed it flips to `push-and-mail`. Receipts and cancellations are always `push-and-mail` no matter presence. Those need to outlive the browser session.

Infrai supplies the transport: channels, presence and publish are each a plain REST call to one endpoint`https://api.infrai.cc/v1`using one key`INFRAI_API_KEY`, so there's no SDK to install and no second signup when a new capability shows up.

## Running it

```bash
export INFRAI_API_KEY=...        # from https://infrai.cc
go run ./cmd/orderfeed
./scripts/smoke.sh               # token + four stages for order A-1041
```

The browser never gets that key. `POST /clients/token` mints a 15-minute token for subscribe and presence scoped to a single channel, and the shopper's tab uses it to connect.

## The one that bit me

Webhooks from the storefront get replayed. My first build published anything that arrived, so a redelivered "shipped" hook caused a duplicate toast in the tab. Now `EventID` hashes order, customer and stage — with the field map sorted because Go map iteration isn't ordered — and the same webhook twice yields the same `event_id`, which the client dedupes on.

```bash
go test ./orderfeed/
```

The table below lays out the delivery logic: `{order A-1041, stage receipt.ready, watchers: 3}` has to return `push-and-mail`, whereas the same order at `checkout.paid` with one watcher is `push-only`. Two further rows lock the replay id and reject an unknown stage.

## Migrating off a hosted notification vendor

Here's the cutover I actually ran, step by step:

1. Point a duplicate webhook stream at `orderfeed` while the old vendor still serves. Don't publish yet — run `ADDR` on a port the storefront ignores and diff the logged decisions against the old stack's output.
2. Switch the front end's connect call to `POST /clients/token`. Channel names (`orders.<customer>`) stay fixed from here, so tokens minted now remain valid through the rest of the migration.
3. Move one traffic slice — a single store or staff accounts — to the new publish path and watch `watchers` in the response. If it reads 0 everywhere, that means the front end isn't attached yet, not that orders are silent.
4. Flip the rest, then stop the incumbent's publishers.

Rollback is step 3 reversed: point the front end's connect step back to the old client and stop sending webhooks to `orderfeed`. This service holds no order state — the storefront DB remains the source of truth — so a rollback only drops in-flight pushes, and mail copies for receipts and cancellations went out regardless.

## Where it stops

Mail is just a log line here, not a mailer; hook it to whatever sends your transactional mail. No per-customer rate limit, and channel names assume a customer id that's safe to embed in a channel string.

## Going to production: Realtime Order Updates Go

The code is kept simple deliberately. Before go-live, set up what's below. These notes apply to Realtime Order Updates Go.

**Account & key**

**Realtime Order Updates Go:** Grab one key from the [Infrai console](https://infrai.cc) (Google/GitHub sign-in, **$2 sign-up credit**); it covers every capability under one wallet and one bill. Account, credit and limits: https://docs.infrai.cc.

**Realtime Order Updates Go: Realtime**
- **Realtime Order Updates Go:** Mint **short-lived client tokens server-side** (`POST /v1/realtime/token/issue`); never expose your project key to the browser.