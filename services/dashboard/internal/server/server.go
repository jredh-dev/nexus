// Package server implements the dashboard HTTP server.
//
// Routes:
//
//	GET /          — serves the dashboard HTML page (embedded)
//	GET /events    — SSE stream of decrypted realtime events from Kafka
//	GET /tiles     — proxies GET /tiles from the digest service
//	GET /health    — liveness probe
//
// The server runs a single background Kafka consumer that fans out to all
// connected SSE clients via an in-process hub. New subscribers receive the
// last N events from a ring buffer before live events begin.
package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/segmentio/kafka-go"

	kafkatypes "github.com/jredh-dev/nexus/internal/kafka"
)

// Config holds runtime configuration for the server.
type Config struct {
	KafkaAddr    string // broker address, e.g. "kafka:9092"
	KafkaTopic   string // topic to consume, e.g. "realtime"
	DigestAddr   string // base URL of the digest service, e.g. "http://nexus-digest:8096"
	Key          []byte // 32-byte AES-256-GCM key
	EventBufSize int    // ring buffer capacity for replay on new connections
}

// sseEvent is the data pushed to SSE clients.
type sseEvent struct {
	TraceID   string            `json:"trace_id"`
	Timestamp time.Time         `json:"ts"`
	Source    string            `json:"src"`
	Level     string            `json:"level"`
	Message   string            `json:"msg"`
	Fields    map[string]string `json:"fields,omitempty"`
}

// Server is the HTTP handler for the dashboard.
type Server struct {
	cfg    Config
	mux    *http.ServeMux
	hub    *hub
	client *http.Client
}

// New creates a Server and starts the background Kafka consumer.
func New(cfg Config) *Server {
	if cfg.EventBufSize <= 0 {
		cfg.EventBufSize = 200
	}
	h := newHub(cfg.EventBufSize)
	s := &Server{
		cfg: cfg,
		hub: h,
		mux: http.NewServeMux(),
		client: &http.Client{
			Timeout: 5 * time.Second,
		},
	}
	s.routes()
	go s.runConsumer()
	return s
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}

func (s *Server) routes() {
	s.mux.HandleFunc("/health", s.handleHealth)
	s.mux.HandleFunc("/events", s.handleSSE)
	s.mux.HandleFunc("/tiles", s.handleTiles)
	s.mux.HandleFunc("/", s.handleIndex)
}

// handleHealth returns 200 OK.
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, "OK")
}

// handleIndex serves the embedded dashboard HTML.
func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(dashboardHTML)
}

// handleSSE streams realtime events to the browser as Server-Sent Events.
// On connect, the last N buffered events are replayed, then live events follow.
func (s *Server) handleSSE(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no") // disable nginx buffering if proxied

	// Subscribe to the hub — get replay buffer + live channel.
	ch, cancel := s.hub.subscribe()
	defer cancel()

	for {
		select {
		case <-r.Context().Done():
			return
		case ev, ok := <-ch:
			if !ok {
				return
			}
			data, err := json.Marshal(ev)
			if err != nil {
				continue
			}
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
		}
	}
}

// handleTiles proxies GET /tiles from the digest service and forwards the
// JSON response to the browser. The browser polls this endpoint directly.
func (s *Server) handleTiles(w http.ResponseWriter, r *http.Request) {
	resp, err := s.client.Get(s.cfg.DigestAddr + "/tiles")
	if err != nil {
		http.Error(w, "digest unavailable", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.WriteHeader(resp.StatusCode)

	var buf [4096]byte
	for {
		n, err := resp.Body.Read(buf[:])
		if n > 0 {
			w.Write(buf[:n])
		}
		if err != nil {
			break
		}
	}
}

// runConsumer is a long-running goroutine that reads from Kafka, decrypts
// each envelope, and publishes decoded events to the hub.
func (s *Server) runConsumer() {
	reader := kafka.NewReader(kafka.ReaderConfig{
		Brokers:        []string{s.cfg.KafkaAddr},
		Topic:          s.cfg.KafkaTopic,
		GroupID:        "dashboard-consumer",
		MinBytes:       1,
		MaxBytes:       1 << 20, // 1 MiB
		CommitInterval: time.Second,
		StartOffset:    kafka.LastOffset, // only live events; replay comes from ring buffer
	})
	defer reader.Close()

	log.Printf("consumer: reading from %s topic=%s", s.cfg.KafkaAddr, s.cfg.KafkaTopic)

	for {
		msg, err := reader.ReadMessage(context.Background())
		if err != nil {
			log.Printf("consumer: read error: %v — retrying in 2s", err)
			time.Sleep(2 * time.Second)
			continue
		}

		// Decode JSON envelope.
		var env kafkatypes.Envelope
		if err := json.Unmarshal(msg.Value, &env); err != nil {
			log.Printf("consumer: unmarshal envelope: %v", err)
			continue
		}

		// Decrypt inner event.
		plaintext, err := kafkatypes.Decrypt(s.cfg.Key, env.Payload, env.Nonce)
		if err != nil {
			log.Printf("consumer: decrypt: %v", err)
			continue
		}

		var ev kafkatypes.Event
		if err := json.Unmarshal(plaintext, &ev); err != nil {
			log.Printf("consumer: unmarshal event: %v", err)
			continue
		}

		s.hub.publish(sseEvent{
			TraceID:   env.TraceID,
			Timestamp: env.Timestamp,
			Source:    env.Source,
			Level:     ev.Level,
			Message:   ev.Message,
			Fields:    ev.Fields,
		})
	}
}

// ---------------------------------------------------------------------------
// hub — fan-out broadcaster with ring buffer for replay
// ---------------------------------------------------------------------------

type hub struct {
	mu      sync.Mutex
	buf     []sseEvent // ring buffer for replay on new connections
	bufSize int
	bufHead int // next write position (wraps)
	bufLen  int // number of valid entries
	subs    map[chan sseEvent]struct{}
}

func newHub(bufSize int) *hub {
	return &hub{
		buf:     make([]sseEvent, bufSize),
		bufSize: bufSize,
		subs:    make(map[chan sseEvent]struct{}),
	}
}

// subscribe registers a new subscriber. Returns a channel that receives events
// and a cancel function to unsubscribe. The channel is pre-loaded with buffered
// historical events before live events begin.
func (h *hub) subscribe() (<-chan sseEvent, func()) {
	ch := make(chan sseEvent, 64)

	h.mu.Lock()
	// Replay the ring buffer (oldest → newest) into the channel before
	// registering for live events, so the client gets history without a gap.
	if h.bufLen > 0 {
		start := (h.bufHead - h.bufLen + h.bufSize) % h.bufSize
		for i := 0; i < h.bufLen; i++ {
			ev := h.buf[(start+i)%h.bufSize]
			select {
			case ch <- ev:
			default: // drop if channel full (shouldn't happen with bufSize=64 and bufLen≤200)
			}
		}
	}
	h.subs[ch] = struct{}{}
	h.mu.Unlock()

	cancel := func() {
		h.mu.Lock()
		delete(h.subs, ch)
		h.mu.Unlock()
		close(ch)
	}
	return ch, cancel
}

// publish writes ev to the ring buffer and fans out to all subscribers.
func (h *hub) publish(ev sseEvent) {
	h.mu.Lock()
	defer h.mu.Unlock()

	// Write to ring buffer.
	h.buf[h.bufHead] = ev
	h.bufHead = (h.bufHead + 1) % h.bufSize
	if h.bufLen < h.bufSize {
		h.bufLen++
	}

	// Fan out to subscribers (non-blocking; slow clients drop events).
	for ch := range h.subs {
		select {
		case ch <- ev:
		default:
		}
	}
}
