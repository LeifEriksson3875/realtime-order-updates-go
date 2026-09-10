package orderfeed

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
)

// Stage is a point in the order lifecycle that the shopper hears about.
type Stage string

const (
	CheckoutPaid       Stage = "checkout.paid"
	FulfillmentPacked  Stage = "fulfillment.packed"
	FulfillmentShipped Stage = "fulfillment.shipped"
	ReceiptReady       Stage = "receipt.ready"
	OrderCanceled      Stage = "order.canceled"
)

// Update is what the storefront hands us.
type Update struct {
	OrderID  string
	Customer string
	Stage    Stage
	Fields   map[string]string
}

// Delivery says what happens to an update after the push attempt.
type Delivery string

const (
	// PushOnly: the shopper has the order page open, the tab is the receipt.
	PushOnly Delivery = "push-only"
	// PushAndMail: nobody is watching, or the update is one they must keep.
	PushAndMail Delivery = "push-and-mail"
)

// Plan is the routing decision for a single update.
type Plan struct {
	Channel  string
	Event    string
	EventID  string
	Delivery Delivery
	Data     map[string]any
}

var ErrUnknownStage = errors.New("orderfeed: unknown stage")

// Route decides where an update goes. Two rules carry the whole thing:
// a receipt or a cancellation is always mailed as well, because the shopper
// needs it after the tab is closed; everything else is push-only while
// someone is attached to the channel, and mailed when nobody is.
func Route(u Update, watchers int) (Plan, error) {
	if u.OrderID == "" || u.Customer == "" {
		return Plan{}, fmt.Errorf("orderfeed: order id and customer are required")
	}

	var event string
	switch u.Stage {
	case CheckoutPaid:
		event = "checkout_paid"
	case FulfillmentPacked:
		event = "fulfillment_packed"
	case FulfillmentShipped:
		event = "fulfillment_shipped"
	case ReceiptReady:
		event = "receipt_ready"
	case OrderCanceled:
		event = "order_canceled"
	default:
		return Plan{}, fmt.Errorf("%w: %q", ErrUnknownStage, u.Stage)
	}

	delivery := PushOnly
	if watchers < 1 || u.Stage == ReceiptReady || u.Stage == OrderCanceled {
		delivery = PushAndMail
	}

	id := EventID(u)
	data := map[string]any{
		"event_id": id,
		"order_id": u.OrderID,
		"stage":    string(u.Stage),
	}
	for k, v := range u.Fields {
		data[k] = v
	}

	return Plan{
		Channel:  Channel(u.Customer),
		Event:    event,
		EventID:  id,
		Delivery: delivery,
		Data:     data,
	}, nil
}

// Channel is the shopper's private channel name.
func Channel(customer string) string {
	return "orders." + strings.ToLower(customer)
}

// EventID is derived from the update itself, so replaying the same webhook
// produces the same id and the tab renders one notification.
func EventID(u Update) string {
	keys := make([]string, 0, len(u.Fields))
	for k := range u.Fields {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	h := sha256.New()
	fmt.Fprintf(h, "%s|%s|%s", u.OrderID, u.Customer, u.Stage)
	for _, k := range keys {
		fmt.Fprintf(h, "|%s=%s", k, u.Fields[k])
	}
	return "evt_" + hex.EncodeToString(h.Sum(nil))[:24]
}
