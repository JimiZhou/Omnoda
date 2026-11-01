package node

import (
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// wsWriter serializes every write on one connection.
// Gorilla websocket connections must not be written concurrently.
type wsWriter struct {
	conn *websocket.Conn
	mu   sync.Mutex
}

func (w *wsWriter) WriteJSON(v any) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	_ = w.conn.SetWriteDeadline(time.Now().Add(15 * time.Second))
	return w.conn.WriteJSON(v)
}
