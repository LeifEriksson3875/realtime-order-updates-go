package orderfeed

import (
	"errors"
	"testing"
)

func TestRouteDelivery(t *testing.T) {
	base := Update{OrderID: "A-1041", Customer: "nadia"}

	cases := []struct {
		name     string
		stage    Stage
		watchers int
		event    string
		delivery Delivery
	}{
		{"paid while watching", CheckoutPaid, 1, "checkout_paid", PushOnly},
		{"paid with tab closed", CheckoutPaid, 0, "checkout_paid", PushAndMail},
		{"packed while watching", FulfillmentPacked, 2, "fulfillment_packed", PushOnly},
		{"shipped with tab closed", FulfillmentShipped, 0, "fulfillment_shipped", PushAndMail},
		{"receipt always mailed", ReceiptReady, 3, "receipt_ready", PushAndMail},
		{"cancellation always mailed", OrderCanceled, 3, "order_canceled", PushAndMail},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			u := base
			u.Stage = tc.stage
			plan, err := Route(u, tc.watchers)
			if err != nil {
				t.Fatalf("Route: %v", err)
			}
			if plan.Event != tc.event {
				t.Errorf("event = %q, want %q", plan.Event, tc.event)
			}
			if plan.Delivery != tc.delivery {
				t.Errorf("delivery = %q, want %q", plan.Delivery, tc.delivery)
			}
			if plan.Channel != "orders.nadia" {
				t.Errorf("channel = %q, want orders.nadia", plan.Channel)
			}
		})
	}
}

func TestEventIDIsStableAcrossRetries(t *testing.T) {
	u := Update{
		OrderID:  "A-1041",
		Customer: "nadia",
		Stage:    FulfillmentShipped,
		Fields:   map[string]string{"carrier": "ups", "tracking": "1Z999"},
	}
	same := Update{
		OrderID:  "A-1041",
		Customer: "nadia",
		Stage:    FulfillmentShipped,
		Fields:   map[string]string{"tracking": "1Z999", "carrier": "ups"},
	}
	if EventID(u) != EventID(same) {
		t.Fatalf("replayed webhook produced a different id: %s vs %s", EventID(u), EventID(same))
	}

	later := u
	later.Stage = ReceiptReady
	if EventID(u) == EventID(later) {
		t.Fatal("distinct stages collided on one id")
	}
}

func TestRouteRejectsBadInput(t *testing.T) {
	if _, err := Route(Update{OrderID: "A-1", Customer: "nadia", Stage: "refund.pending"}, 1); !errors.Is(err, ErrUnknownStage) {
		t.Fatalf("err = %v, want ErrUnknownStage", err)
	}
	if _, err := Route(Update{Stage: CheckoutPaid, Customer: "nadia"}, 1); err == nil {
		t.Fatal("expected an error for a missing order id")
	}
}
