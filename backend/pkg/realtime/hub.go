package realtime

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/google/uuid"
)

// ClientInfo is the authenticated identity attached to a socket by the caller
// (cmd/api) after validating the session cookie. The hub trusts it and never
// reads credentials itself.
type ClientInfo struct {
	WorldID   uuid.UUID
	ManagerID uuid.UUID
}

// Hub owns every connected socket and routes broker events to the clients
// registered for the event's world. Screens never open their own sockets; the
// Nuxt useSocket() composable holds the single connection and fans events out.
type Hub struct {
	broker         Broker
	logger         *log.Logger
	originPatterns []string
	readLimit      int64
	pingInterval   time.Duration
	writeTimeout   time.Duration

	mu      sync.RWMutex
	clients map[uuid.UUID]map[*client]struct{}
}

// Option customises a Hub.
type Option func(*Hub)

// WithOriginPatterns restricts which browser origins may open a socket. A bare
// `go run` with no APP_ORIGIN leaves this empty, which makes the upgrader fall
// back to same-host origin checks.
func WithOriginPatterns(patterns []string) Option {
	return func(h *Hub) { h.originPatterns = patterns }
}

// WithLogger overrides the package logger.
func WithLogger(l *log.Logger) Option {
	return func(h *Hub) { h.logger = l }
}

// WithPingInterval sets how often the server pings idle sockets.
func WithPingInterval(d time.Duration) Option {
	return func(h *Hub) { h.pingInterval = d }
}

// WithWriteTimeout bounds a single write/ping.
func WithWriteTimeout(d time.Duration) Option {
	return func(h *Hub) { h.writeTimeout = d }
}

// NewHub builds a hub around a broker. Call Run in a goroutine to start
// receiving broker events.
func NewHub(broker Broker, opts ...Option) *Hub {
	h := &Hub{
		broker:       broker,
		logger:       log.Default(),
		readLimit:    1 << 14, // 16 KiB is plenty for client control frames
		pingInterval: 30 * time.Second,
		writeTimeout: 10 * time.Second,
		clients:      make(map[uuid.UUID]map[*client]struct{}),
	}
	for _, opt := range opts {
		opt(h)
	}
	return h
}

// Publish sends an event through the broker to every pod and client.
func (h *Hub) Publish(ctx context.Context, ev Event) error {
	if ev.TS.IsZero() {
		ev.TS = time.Now().UTC()
	}
	return h.broker.Publish(ctx, ev)
}

// Run pumps broker events into locally connected clients until ctx is done.
func (h *Hub) Run(ctx context.Context) error {
	return h.broker.Subscribe(ctx, h.deliver)
}

// Ready reports when the hub's broker can receive events.
func (h *Hub) Ready() <-chan struct{} { return h.broker.Ready() }

// Close releases the broker.
func (h *Hub) Close() error { return h.broker.Close() }

// HandleWS upgrades an HTTP request to a WebSocket, registers the client under
// its world, and blocks until the socket closes. The caller must have already
// authenticated the request; world scoping is enforced per event on delivery.
func (h *Hub) HandleWS(w http.ResponseWriter, r *http.Request, info ClientInfo) {
	if info.WorldID == uuid.Nil {
		http.Error(w, "no world context", http.StatusBadRequest)
		return
	}

	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		OriginPatterns: h.originPatterns,
	})
	if err != nil {
		// Accept has already written an HTTP error response.
		h.logger.Printf("realtime: websocket upgrade rejected: %v", err)
		return
	}
	conn.SetReadLimit(h.readLimit)

	c := &client{
		conn:      conn,
		worldID:   info.WorldID,
		managerID: info.ManagerID,
		send:      make(chan []byte, 64),
	}
	h.add(c)
	defer h.remove(c)

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	writeDone := make(chan struct{})
	go func() {
		defer close(writeDone)
		c.writePump(ctx, h)
	}()

	h.readLoop(ctx, c)

	cancel()
	<-writeDone
	c.close(websocket.StatusNormalClosure, "")
}

func (h *Hub) readLoop(ctx context.Context, c *client) {
	for {
		typ, data, err := c.conn.Read(ctx)
		if err != nil {
			return
		}
		if typ != websocket.MessageText {
			continue
		}
		h.handleClientMessage(c, data)
	}
}

// handleClientMessage implements the tiny client->server protocol: ping is
// answered with pong; anything else is reported as a typed error and ignored
// so a misbehaving client cannot tear down its own connection.
func (h *Hub) handleClientMessage(c *client, data []byte) {
	var msg struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(data, &msg); err != nil {
		c.sendEvent(errorPayload("invalid_message", "message is not valid JSON"))
		return
	}

	switch msg.Type {
	case "ping":
		c.sendEvent(MustEvent(EventPong, uuid.Nil, nil))
	case "":
		c.sendEvent(errorPayload("missing_type", "message is missing a type"))
	default:
		c.sendEvent(errorPayload("unknown_type", "unsupported message type "+msg.Type))
	}
}

func errorPayload(code, message string) Event {
	return MustEvent(EventError, uuid.Nil, map[string]string{"code": code, "message": message})
}

func (h *Hub) add(c *client) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.clients[c.worldID] == nil {
		h.clients[c.worldID] = make(map[*client]struct{})
	}
	h.clients[c.worldID][c] = struct{}{}
}

func (h *Hub) remove(c *client) {
	h.mu.Lock()
	if set := h.clients[c.worldID]; set != nil {
		delete(set, c)
		if len(set) == 0 {
			delete(h.clients, c.worldID)
		}
	}
	h.mu.Unlock()
}

// deliver encodes ev once and fans it out to clients registered for its world.
// Events without a world are dropped: they are never broadcast across worlds.
func (h *Hub) deliver(ev Event) {
	if ev.WorldID == nil {
		return
	}
	data, err := json.Marshal(ev)
	if err != nil {
		h.logger.Printf("realtime: dropping unmarshalable event %s: %v", ev.Type, err)
		return
	}

	h.mu.RLock()
	clients := make([]*client, 0, len(h.clients[*ev.WorldID]))
	for c := range h.clients[*ev.WorldID] {
		clients = append(clients, c)
	}
	h.mu.RUnlock()

	for _, c := range clients {
		c.enqueue(data)
	}
}

// WorldClientCount reports how many sockets are registered for a world. Used by
// tests and future admin tooling.
func (h *Hub) WorldClientCount(worldID uuid.UUID) int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.clients[worldID])
}

type client struct {
	conn      *websocket.Conn
	worldID   uuid.UUID
	managerID uuid.UUID
	send      chan []byte

	closeOnce sync.Once
}

// enqueue hands data to the write pump without blocking. A client that cannot
// keep up drops events rather than stalling the hub.
func (c *client) enqueue(data []byte) bool {
	select {
	case c.send <- data:
		return true
	default:
		return false
	}
}

func (c *client) sendEvent(ev Event) {
	if ev.TS.IsZero() {
		ev.TS = time.Now().UTC()
	}
	data, err := json.Marshal(ev)
	if err != nil {
		return
	}
	c.enqueue(data)
}

func (c *client) writePump(ctx context.Context, h *Hub) {
	ticker := time.NewTicker(h.pingInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case data := <-c.send:
			wctx, cancel := context.WithTimeout(ctx, h.writeTimeout)
			err := c.conn.Write(wctx, websocket.MessageText, data)
			cancel()
			if err != nil {
				return
			}
		case <-ticker.C:
			pctx, cancel := context.WithTimeout(ctx, h.writeTimeout)
			err := c.conn.Ping(pctx)
			cancel()
			if err != nil {
				return
			}
		}
	}
}

func (c *client) close(code websocket.StatusCode, reason string) {
	c.closeOnce.Do(func() {
		_ = c.conn.Close(code, reason)
	})
}
