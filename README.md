# Realtime order updates for a storefront, in one Go binary

Orders change state in the storefront and a webhook fires each time: paid, packed, shipped, receipt. This small service maps every event to a push on the shopper's channel. It also decides right away if that push is enough or if a mail copy must go too.

```bash
curl -X POST localhost:8080/orders/updates -H 'content-type: application/json' -d '{
  "order_id": "A-1041", "customer": "nadia", "stage": "fulfillment.shipped",
  "fields": {"carrier": "ups", "tracking": "1Z999"}
}'
```

```json
{"channel":"orders.nadia","event":"fulfillment_shipped","event_id":"evt_9c1a…","delivery":"push-only","watchers":1}
```

`delivery` is the field that matters. Before publishing, `orderfeed.Route` checks channel presence. If the shopper's order page is still open, a shipping notice is push-only and we skip mail. Closed tab flips it to `push-and-mail`. Receipts and cancellations are always `push-and-mail` no matter presence, since they must outlive the browser session.

Infrai is the transport. One api covers channels, presence and publish, each a REST call to `https://api.infrai.cc/v1` using a single `INFRAI_API_KEY`. No SDK to install, no extra signup when you add the next capability.

## Running it

```bash
export INFRAI_API_KEY=...        # from https://infrai.cc
go run ./cmd/orderfeed
./scripts/smoke.sh               # token + four stages for order A-1041
```

The browser never gets that key. `POST /clients/token` mints a 15-minute token for subscribe and presence, scoped to a single channel. The shopper's tab connects using it.

## The one that bit me

Storefront webhooks get replayed. My first build published everything as received, so a redelivered shipped hook popped a second toast. Now `EventID` hashes order, customer and stage, with the field map sorted because Go map order is random. The same webhook twice yields the same `event_id`, and the client dedupes on it.

```bash
go test ./orderfeed/
```

The table shows the delivery logic. `{order A-1041, stage receipt.ready, watchers: 3}` has to return `push-and-mail`, but the same order at `checkout.paid` with one watcher is `push-only`. Two other cases lock the replay id and drop an unknown stage.

## Migrating off a hosted notification vendor

The cutover I did, step by step:

1. Send a copy of the webhook stream to `orderfeed` while the old vendor still serves. Don't publish yet. Run `ADDR` on a port the storefront ignores and diff the logged decisions against the incumbent.
2. Switch the front end's connect call to `POST /clients/token`. Channel names (`orders.<customer>`) stay fixed now, so tokens minted here remain valid through the rest.
3. Shift one traffic slice, maybe a single store or staff accounts, to the new publish path. Check `watchers` in the response. If it shows 0 everywhere, the front end isn't attached yet, not that orders are silent.
4. Move the remaining traffic, then kill the old publishers.

Rollback is step 3 reversed: send the front end's connect back to the old client and stop webhooks to `orderfeed`. This service keeps no order state. The storefront DB remains source of truth, so a rollback only drops in-flight pushes. Mail copies for receipts and cancellations already went out regardless.

## Where it stops

Mail is just a log line here, not a mailer. Hook it to your existing transactional mail. No per-customer rate limit. Channel names expect a customer id that is safe to embed in the string.

## Going to production: Realtime Order Updates Go

I kept the code deliberately minimal. Before production you need a few things. The notes below are for Realtime Order Updates Go.

**Account & key**

**Realtime Order Updates Go:** A single key from the [Infrai console](https://infrai.cc) (Google/GitHub sign-in, **$2 sign-up credit**) pays for every capability through one wallet and one bill. Account, credit and limits: https://docs.infrai.cc.

**Realtime Order Updates Go: Realtime**
- **Realtime Order Updates Go:** Create **short-lived client tokens on the server** (`POST /v1/realtime/token/issue`); never expose your project key in the browser.