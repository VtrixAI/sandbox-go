package sandbox

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/gorilla/websocket"
)

// Sandbox is a live connection to a single sandbox instance.
// It is safe to call its methods from multiple goroutines.
type Sandbox struct {
	Info Info

	client     *Client
	conn       *websocket.Conn
	idGen      atomic.Int64
	mu         sync.Mutex // guards pending
	pending    map[int64]*pendingCall
	closed     chan struct{}
	closeOnce  sync.Once
	defaultEnv map[string]string // inherited by all exec calls
}

type pendingCall struct {
	// ch receives exactly one *rpcResponse (the final result/error message).
	ch chan *rpcResponse
	// stream, when non-nil, receives all exec.* notification events; closed when done.
	stream chan ExecEvent
	// notif, when non-nil, receives all generic RPC notifications for this call.
	notif chan rpcResponse
}

// readLoop pumps incoming WS messages and dispatches them to waiters.
func (s *Sandbox) readLoop() {
	defer func() {
		s.closeOnce.Do(func() { close(s.closed) })

		s.mu.Lock()
		for id, p := range s.pending {
			close(p.ch)
			if p.stream != nil {
				close(p.stream)
			}
			if p.notif != nil {
				close(p.notif)
			}
			delete(s.pending, id)
		}
		s.mu.Unlock()
	}()

	for {
		_, raw, err := s.conn.ReadMessage()
		if err != nil {
			return
		}

		var msg rpcResponse
		if err := json.Unmarshal(raw, &msg); err != nil {
			continue
		}

		// Notification (exec.stdout / exec.stderr / exec.done / exec.start)
		if msg.Method != "" {
			s.dispatchNotification(&msg)
			continue
		}

		// Response
		if msg.ID == nil {
			continue
		}
		s.mu.Lock()
		p, ok := s.pending[*msg.ID]
		if ok {
			delete(s.pending, *msg.ID)
		}
		s.mu.Unlock()
		if ok {
			if p.stream != nil {
				close(p.stream)
			}
			if p.notif != nil {
				close(p.notif)
			}
			p.ch <- &msg
		}
	}
}

func (s *Sandbox) dispatchNotification(msg *rpcResponse) {
	var np execNotifParams
	if msg.Params != nil {
		_ = json.Unmarshal(msg.Params, &np)
	}

	var targetID int64
	switch v := np.ID.(type) {
	case float64:
		targetID = int64(v)
	case int64:
		targetID = v
	default:
		return
	}

	s.mu.Lock()
	p, ok := s.pending[targetID]
	s.mu.Unlock()
	if !ok {
		return
	}

	if p.stream != nil {
		var ev ExecEvent
		switch msg.Method {
		case "exec.start":
			ev = ExecEvent{Type: "start"}
		case "exec.stdout":
			ev = ExecEvent{Type: "stdout", Data: np.Data}
		case "exec.stderr":
			ev = ExecEvent{Type: "stderr", Data: np.Data}
		case "exec.done":
			ev = ExecEvent{Type: "done", Data: np.Output}
		default:
			return
		}
		select {
		case p.stream <- ev:
		default:
			// consumer is slow; block briefly rather than silently drop
			go func() { p.stream <- ev }()
		}
		return
	}

	if p.notif != nil {
		// non-blocking send: if consumer is slow, drop rather than deadlock
		select {
		case p.notif <- *msg:
		default:
		}
	}
}

// call sends a JSON-RPC request and waits for the response.
// Pass a non-nil stream to also receive streaming exec.* notifications.
func (s *Sandbox) call(ctx context.Context, method string, params any, stream chan ExecEvent) (*rpcResponse, error) {
	p := &pendingCall{
		ch:     make(chan *rpcResponse, 1),
		stream: stream,
	}
	return s.sendAndWait(ctx, method, params, p)
}

// callWithNotif sends a JSON-RPC request; notifications are delivered to notif channel.
func (s *Sandbox) callWithNotif(ctx context.Context, method string, params any, notif chan rpcResponse) (*rpcResponse, error) {
	p := &pendingCall{
		ch:    make(chan *rpcResponse, 1),
		notif: notif,
	}
	return s.sendAndWait(ctx, method, params, p)
}

// sendAndWait registers p, sends the request, and waits for the response.
func (s *Sandbox) sendAndWait(ctx context.Context, method string, params any, p *pendingCall) (*rpcResponse, error) {
	select {
	case <-s.closed:
		return nil, fmt.Errorf("sandbox connection closed")
	default:
	}

	id := s.idGen.Add(1)
	req := rpcRequest{
		Jsonrpc: "2.0",
		Method:  method,
		Params:  params,
		ID:      &id,
	}

	s.mu.Lock()
	s.pending[id] = p
	s.mu.Unlock()

	data, err := json.Marshal(req)
	if err != nil {
		s.removePending(id)
		return nil, err
	}

	if err := s.conn.WriteMessage(websocket.TextMessage, data); err != nil {
		s.removePending(id)
		return nil, fmt.Errorf("ws write: %w", err)
	}

	select {
	case resp, ok := <-p.ch:
		if !ok {
			return nil, fmt.Errorf("sandbox connection closed while waiting for response")
		}
		if resp.Error != nil {
			return nil, resp.Error
		}
		return resp, nil
	case <-ctx.Done():
		s.removePending(id)
		return nil, ctx.Err()
	case <-s.closed:
		return nil, fmt.Errorf("sandbox connection closed")
	}
}

func (s *Sandbox) removePending(id int64) {
	s.mu.Lock()
	delete(s.pending, id)
	s.mu.Unlock()
}

// Close closes the underlying WebSocket connection.
func (s *Sandbox) Close() error {
	return s.conn.Close()
}
