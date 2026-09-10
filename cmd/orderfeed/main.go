// Command orderfeed is the single binary that stands between the storefront's
// webhooks and the shopper's open tab.
package main

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"

	"github.com/notes-to-self/order-notify/infrai"
	"github.com/notes-to-self/order-notify/orderfeed"
)

type server struct {
	rt *infrai.Client
}

type updateRequest struct {
	OrderID  string            `json:"order_id"`
	Customer string            `json:"customer"`
	Stage    string            `json:"stage"`
	Fields   map[string]string `json:"fields"`
}

func main() {
	key := os.Getenv("INFRAI_API_KEY")
	if key == "" {
		log.Fatal("set INFRAI_API_KEY (a new account starts with a $2 credit and is billed per use)")
	}
	client := infrai.New(key)
	if base := os.Getenv("INFRAI_BASE_URL"); base != "" {
		client.BaseURL = base
	}
	s := &server{rt: client}

	mux := http.NewServeMux()
	mux.HandleFunc("POST /orders/updates", s.handleUpdate)
	mux.HandleFunc("POST /clients/token", s.handleToken)

	addr := os.Getenv("ADDR")
	if addr == "" {
		addr = ":8080"
	}
	log.Printf("orderfeed listening on %s", addr)
	log.Fatal(http.ListenAndServe(addr, mux))
}

// handleUpdate takes one storefront webhook, asks who is attached to the
// shopper's channel, and pushes the update the way Route decided.
func (s *server) handleUpdate(w http.ResponseWriter, r *http.Request) {
	var req updateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json body", http.StatusBadRequest)
		return
	}
	u := orderfeed.Update{
		OrderID:  req.OrderID,
		Customer: req.Customer,
		Stage:    orderfeed.Stage(req.Stage),
		Fields:   req.Fields,
	}

	// Validate the update before creating the persistent channel. Invalid
	// webhooks must not leave an undeletable channel behind.
	if _, err := orderfeed.Route(u, 0); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	channel := orderfeed.Channel(req.Customer)
	if err := s.rt.CreateChannel(channel, "private"); err != nil {
		s.fail(w, err)
		return
	}

	watchers, err := s.watchers(channel)
	if err != nil {
		s.fail(w, err)
		return
	}

	plan, err := orderfeed.Route(u, watchers)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := s.rt.Publish(plan.Channel, plan.Event, plan.Data); err != nil {
		s.fail(w, err)
		return
	}
	if plan.Delivery == orderfeed.PushAndMail {
		log.Printf("queued mail copy order=%s event=%s id=%s", u.OrderID, plan.Event, plan.EventID)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"channel":  plan.Channel,
		"event":    plan.Event,
		"event_id": plan.EventID,
		"delivery": string(plan.Delivery),
		"watchers": watchers,
	})
}

// handleToken hands the browser a short-lived subscribe-only token for its
// own channel. The service key stays on this side of the wire.
func (s *server) handleToken(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Customer string `json:"customer"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Customer == "" {
		http.Error(w, "customer is required", http.StatusBadRequest)
		return
	}
	channel := orderfeed.Channel(req.Customer)
	if err := s.rt.CreateChannel(channel, "private"); err != nil {
		s.fail(w, err)
		return
	}
	data, err := s.rt.IssueToken(req.Customer, []string{channel}, []string{"subscribe", "presence"}, 900)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, json.RawMessage(data))
}

type presenceView struct {
	Count   *int              `json:"count"`
	Members []json.RawMessage `json:"members"`
}

func (s *server) watchers(channel string) (int, error) {
	data, err := s.rt.Presence(channel)
	if err != nil {
		return 0, err
	}
	var view presenceView
	if err := json.Unmarshal(data, &view); err != nil {
		return 0, nil
	}
	if view.Count != nil {
		return *view.Count, nil
	}
	return len(view.Members), nil
}

// fail keeps an answer from the realtime API an answer: a rejected argument
// reaches our caller as a 4xx with its code, not as a blanket 500.
func (s *server) fail(w http.ResponseWriter, err error) {
	var apiErr *infrai.APIError
	if errors.As(err, &apiErr) && apiErr.Status >= 400 && apiErr.Status < 500 {
		writeJSON(w, apiErr.Status, map[string]any{"code": apiErr.Code, "message": apiErr.Message})
		return
	}
	log.Printf("upstream: %v", err)
	writeJSON(w, http.StatusBadGateway, map[string]any{"code": "UPSTREAM_UNAVAILABLE"})
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(body)
}
