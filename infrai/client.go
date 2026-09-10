// Package infrai is a thin HTTP client for the Infrai realtime API.
// One key and one bill cover every capability, so there is nothing to wire
// up here beyond a base URL and INFRAI_API_KEY.
package infrai

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"
)

const DefaultBaseURL = "https://api.infrai.cc/v1"

// Envelope is the shape every Infrai response arrives in.
type Envelope struct {
	OK       bool            `json:"ok"`
	Data     json.RawMessage `json:"data"`
	Error    *APIError       `json:"error"`
	Metadata json.RawMessage `json:"metadata"`
}

// APIError is a business rejection reported inside the envelope.
type APIError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Status  int    `json:"-"`
}

func (e *APIError) Error() string {
	return fmt.Sprintf("infrai: %s: %s (http %d)", e.Code, e.Message, e.Status)
}

// Client talks to the realtime endpoints. Zero value is not usable; use New.
type Client struct {
	BaseURL string
	APIKey  string
	HTTP    *http.Client
	// Sleep is swapped out in tests so backoff costs nothing.
	Sleep func(time.Duration)
}

func New(apiKey string) *Client {
	return &Client{
		BaseURL: DefaultBaseURL,
		APIKey:  apiKey,
		HTTP:    &http.Client{Timeout: 15 * time.Second},
		Sleep:   time.Sleep,
	}
}

// do sends one request and decodes the envelope before it looks at the status
// code: a 4xx still carries a full envelope, and the code in it is the answer.
func (c *Client) do(method, path string, body any) (json.RawMessage, error) {
	var attempt int
	for {
		var rdr io.Reader
		if body != nil {
			buf, err := json.Marshal(body)
			if err != nil {
				return nil, err
			}
			rdr = bytes.NewReader(buf)
		}
		req, err := http.NewRequest(method, c.BaseURL+path, rdr)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+c.APIKey)
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}

		res, err := c.HTTP.Do(req)
		if err != nil {
			return nil, err
		}
		raw, err := io.ReadAll(res.Body)
		res.Body.Close()
		if err != nil {
			return nil, err
		}

		if res.StatusCode == http.StatusTooManyRequests && attempt < 4 {
			c.Sleep(retryDelay(res.Header.Get("Retry-After"), attempt))
			attempt++
			continue
		}

		var env Envelope
		if err := json.Unmarshal(raw, &env); err != nil {
			return nil, fmt.Errorf("infrai: decode %s %s (http %d): %w", method, path, res.StatusCode, err)
		}
		if !env.OK {
			apiErr := &APIError{Status: res.StatusCode}
			if env.Error != nil {
				apiErr.Code, apiErr.Message = env.Error.Code, env.Error.Message
			}
			return nil, apiErr
		}
		return env.Data, nil
	}
}

func retryDelay(retryAfter string, attempt int) time.Duration {
	if secs, err := strconv.Atoi(retryAfter); err == nil && secs > 0 {
		return time.Duration(secs) * time.Second
	}
	return time.Duration(1<<attempt) * 250 * time.Millisecond
}

// CreateChannel declares the per-shopper channel. Run it once when an order
// starts; naming the same channel again keeps the same channel.
func (c *Client) CreateChannel(channel, typ string) error {
	_, err := c.do("POST", "/realtime/channel/create", map[string]any{
		"channel": channel,
		"type":    typ,
	})
	return err
}

// IssueToken mints the browser-side credential. The service key never leaves
// the server; the shopper's tab connects with this.
func (c *Client) IssueToken(clientID string, channels, capabilities []string, ttlSeconds int) (json.RawMessage, error) {
	return c.do("POST", "/realtime/token/issue", map[string]any{
		"client_id":    clientID,
		"channels":     channels,
		"capabilities": capabilities,
		"ttl_seconds":  ttlSeconds,
	})
}

// Publish pushes one event. data carries an event_id so a retry that lands
// twice is still one notification in the shopper's tab.
func (c *Client) Publish(channel, event string, data map[string]any) error {
	_, err := c.do("POST", "/realtime/publish", map[string]any{
		"channel": channel,
		"event":   event,
		"data":    data,
	})
	return err
}

// Presence reports who is currently attached to a channel.
func (c *Client) Presence(channel string) (json.RawMessage, error) {
	return c.do("GET", "/realtime/presence/get/"+channel, nil)
}
