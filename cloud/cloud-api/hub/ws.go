package hub

import (
	"time"

	"github.com/gorilla/websocket"
	"go.uber.org/zap"
)

// WritePump drains the Connection.Send channel to the WebSocket.
// Sends WebSocket pings at pingPeriod intervals to keep the connection alive.
// Must be run as a goroutine; returns when the channel is closed or a write fails.
func WritePump(c *Connection, log *zap.Logger) {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		c.Conn.Close()
	}()

	for {
		select {
		case msg, ok := <-c.Send:
			c.Conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				// Channel closed — send a clean close frame.
				c.Conn.WriteMessage(websocket.CloseMessage, []byte{}) //nolint:errcheck
				return
			}
			if err := c.Conn.WriteMessage(websocket.TextMessage, msg); err != nil {
				log.Warn("hub ws write error",
					zap.String("hub_id", c.HubID), zap.Error(err))
				return
			}

		case <-ticker.C:
			c.Conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.Conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

// ReadPump reads and discards messages from the hub (hub→cloud data flows via REST ingest).
// Keeps the connection alive by refreshing the read deadline on each pong.
// Must be run as a goroutine; unregisters the connection and closes the Send channel on exit.
func ReadPump(c *Connection, mgr *Manager, log *zap.Logger) {
	defer func() {
		mgr.Unregister(c.HubID)
		c.Conn.Close()
		close(c.Send)
	}()

	c.Conn.SetReadLimit(maxMessageSize)
	c.Conn.SetReadDeadline(time.Now().Add(pongWait))
	c.Conn.SetPongHandler(func(string) error {
		c.Conn.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})

	for {
		_, _, err := c.Conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err,
				websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Warn("hub ws unexpected close",
					zap.String("hub_id", c.HubID), zap.Error(err))
			}
			return
		}
	}
}
