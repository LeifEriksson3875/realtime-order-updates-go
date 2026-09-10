#!/usr/bin/env bash
# Drive one order through the service: mint a browser token, then send the
# four updates a shopper sees between paying and getting the receipt.
set -euo pipefail

HOST="${HOST:-http://localhost:8080}"
CUSTOMER="${CUSTOMER:-nadia}"
ORDER="${ORDER:-A-1041}"

post() { curl -sS -X POST "$HOST$1" -H 'content-type: application/json' -d "$2"; echo; }

post /clients/token "{\"customer\":\"$CUSTOMER\"}"

for stage in checkout.paid fulfillment.packed fulfillment.shipped receipt.ready; do
  post /orders/updates "{\"order_id\":\"$ORDER\",\"customer\":\"$CUSTOMER\",\"stage\":\"$stage\",\"fields\":{\"carrier\":\"ups\",\"tracking\":\"1Z999\"}}"
done
