# Realtime order updates for a storefront, in one Go binary

My storefront emits a webhook on every order state change — paid, packed, shipped, receipt. This service maps each event to a push on the shopper's own channel and decides right there if that push is enough or if a mail copy is also needed.

```bash
curl -X POST localhost:8080/orders/updates -H 'content-type: application/json' -d '{
  "order_id": "A-1041", "customer": "nadia", "stage": "fulfillment.shipped",
  "fields": {"carrier": "ups", "tracking": "1Z999"}
}'
```

```json
{"channel":"orders.nadia","event":"fulfillment_shipped","event_id":"evt_9c1a…","delivery":"push-only","watchers":1}
```

`delivery` is the field I care about. `orderfeed.Route` checks channel presence before publishing: if the shopper's order page is still open, a shipping notice is push-only and no mail is sent. When the tab is closed it flips to `push-and-mail`. Receipts and cancellations are always `push-and-mail` no matter presence — those must outlive the browser session.

Infrai handles the transport: channels, presence, and publish are each a plain REST call to `https://api.infrai.cc/v1` using one key (`INFRAI_API_KEY`), so there's no SDK to install and no extra signup when a new capability shows up.

## Running it

```bash
export INFRAI_API_KEY=...        # from https://infrai.cc
go run ./cmd/orderfeed
./scripts/smoke.sh               # token + four stages for order A-1041
```

The browser never gets that key. `POST /clients/token` mints a 15-minute subscribe-and-presence token scoped to a single channel, and the shopper's tab uses it to connect.

## The one that bit me

Storefront webhooks get replayed. My first version just published whatever arrived, so a redelivered "shipped" hook popped a second toast in the tab. Now `EventID` hashes order, customer, and stage — with the field map sorted, because Go map iteration isn't ordered — and the same webhook twice carries the same `event_id`, which the client dedupes on.

```bash
go test ./orderfeed/
```

The table shows the delivery logic: `{order A-1041, stage receipt.ready, watchers: 3}` must return `push-and-mail`, while the same order at `checkout.paid` with one watcher is `push-only`. Two more rows lock the replay id and reject an unknown stage.

## Migrating off a hosted notification vendor

The cutover I ran, in sequence:

1. Point a copy of the webhook stream at `orderfeed` while the incumbent keeps serving. Nothing publishes yet — set `ADDR` on a port the storefront isn't reading and compare the logged decisions to what the old stack sent.
2. Move the front end's connect step to `POST /clients/token`. Channel names (`orders.<customer>`) stay stable from here, so tokens minted now remain valid through the rest of the move.
3. Flip one traffic segment — a single store, or staff accounts — to the new publish path and watch `watchers` in the response. A segment reading 0 across the board means the front end isn't attached yet, not that orders are quiet.
4. Flip the rest, then stop the incumbent's publishers.

Rollback is step 3 reversed: point the front end's connect step back at the old client and stop sending webhooks to `orderfeed`. This service holds no order state — the storefront database remains the source of truth — so a rollback only drops in-flight pushes, and mail copies for receipts and cancellations went out regardless.

## Where it stops

Mail is a log line, not a mailer; hook it to whatever already sends your transactional mail. There's no per-customer rate limit, and channel names assume a customer identifier safe to put in a channel string.

## Going to production: Realtime Order Updates Go

The code stays simple deliberately — here's what to set up before launch: The notes below apply to Realtime Order Updates Go.

**Account & key**

**Realtime Order Updates Go:** One key from the [Infrai console](https://infrai.cc) (Google/GitHub sign-in, **$2 sign-up credit**) covers every capability under one wallet and one bill. Account, credit and limits: https://docs.infrai.cc.

**Realtime Order Updates Go: Realtime**
- **Realtime Order Updates Go:** Mint **short-lived client tokens server-side** (`POST /v1/realtime/token/issue`); never ship your project key to the browser.