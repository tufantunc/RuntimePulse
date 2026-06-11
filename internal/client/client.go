// Package client is the CLI-side RPC client for the daemon socket.
package client

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"time"

	"github.com/tufantunc/RuntimePulse/internal/core"
)

type Client struct {
	Socket string
}

func New(socket string) *Client { return &Client{Socket: socket} }

type request struct {
	ID     int64  `json:"id"`
	Method string `json:"method"`
	Params any    `json:"params,omitempty"`
}

type response struct {
	ID     int64           `json:"id"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  string          `json:"error,omitempty"`
}

// Call dials, sends one request, decodes one response into out (out may be nil).
func (c *Client) Call(method string, params, out any) error {
	conn, err := net.DialTimeout("unix", c.Socket, time.Second)
	if err != nil {
		return err
	}
	defer conn.Close()
	if err := json.NewEncoder(conn).Encode(request{ID: 1, Method: method, Params: params}); err != nil {
		return err
	}
	var resp response
	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		return err
	}
	if resp.Error != "" {
		return errors.New(resp.Error)
	}
	if out != nil && len(resp.Result) > 0 {
		return json.Unmarshal(resp.Result, out)
	}
	return nil
}

// Follow streams events to fn until ctx is cancelled or the daemon closes.
// Bus semantics: a slow consumer may miss events (no gap signal);
// reconcile via events.list if completeness matters.
func (c *Client) Follow(ctx context.Context, fn func(core.Event)) error {
	conn, err := net.DialTimeout("unix", c.Socket, time.Second)
	if err != nil {
		return err
	}
	defer conn.Close()
	go func() {
		<-ctx.Done()
		conn.Close()
	}()
	if err := json.NewEncoder(conn).Encode(request{ID: 1, Method: "events.follow"}); err != nil {
		return err
	}
	dec := json.NewDecoder(conn)
	var first response
	if err := dec.Decode(&first); err != nil {
		return err
	}
	if first.Error != "" {
		return errors.New(first.Error)
	}
	for {
		var note struct {
			Method string     `json:"method"`
			Params core.Event `json:"params"`
		}
		if err := dec.Decode(&note); err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		if note.Method == "event" {
			fn(note.Params)
		}
	}
}
