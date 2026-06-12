// Package wsserver streams live events over a token-gated WebSocket.
// Loopback-only by construction (spec §9): the bind address is
// hardcoded to 127.0.0.1 and not configurable. Bus semantics apply —
// a slow consumer misses events; durability lives in the store.
package wsserver

import (
	"context"
	"crypto/subtle"
	"fmt"
	"net"
	"net/http"
	"strings"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"

	"github.com/tufantunc/RuntimePulse/internal/bus"
)

// Start listens on 127.0.0.1:port (0 = ephemeral) and serves /events.
// Returns the actual listen address; shuts down when ctx is cancelled.
func Start(ctx context.Context, port int, token string, b *bus.Bus) (string, error) {
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return "", err
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/events", func(w http.ResponseWriter, r *http.Request) {
		got := r.URL.Query().Get("token")
		if got == "" {
			got = strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		}
		if subtle.ConstantTimeCompare([]byte(got), []byte(token)) != 1 {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return // Accept already wrote the HTTP error
		}
		defer conn.CloseNow()
		ch, cancel := b.Subscribe(64)
		defer cancel()
		for {
			select {
			case <-r.Context().Done():
				return
			case ev, ok := <-ch:
				if !ok {
					return
				}
				if err := wsjson.Write(r.Context(), conn, ev); err != nil {
					return
				}
			}
		}
	})
	srv := &http.Server{Handler: mux, BaseContext: func(net.Listener) context.Context { return ctx }}
	go func() {
		<-ctx.Done()
		srv.Close()
	}()
	go srv.Serve(ln)
	return ln.Addr().String(), nil
}
