package client

import (
	"context"
	"encoding/json"
	"net"
	"path/filepath"
	"testing"
	"time"

	"github.com/itsmehatef/dclaw/internal/protocol"
)

func TestLogsStreamFallsBackToPollOnMethodNotFound(t *testing.T) {
	socket := serveLogsFallbackRPC(t)
	c := NewRPCClient(socket)
	t.Cleanup(func() { _ = c.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	ch, err := c.LogsStream(ctx, "alice", 10)
	if err != nil {
		t.Fatalf("LogsStream: %v", err)
	}
	select {
	case ev := <-ch:
		if ev.Err != nil {
			t.Fatalf("unexpected event error: %v", ev.Err)
		}
		if ev.Name != "alice" || ev.Line != "fallback line" || ev.Stream != "stdout" {
			t.Fatalf("unexpected fallback event: %#v", ev)
		}
	case <-ctx.Done():
		t.Fatal("timed out waiting for fallback log line")
	}
	cancel()
}

func serveLogsFallbackRPC(t *testing.T) string {
	t.Helper()
	socket := filepath.Join(t.TempDir(), "dclaw.sock")
	ln, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatalf("listen unix: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })

	done := make(chan error, 1)
	go func() {
		for i := 0; i < 2; i++ {
			conn, err := ln.Accept()
			if err != nil {
				done <- err
				return
			}
			if err := handleLogsFallbackConn(t, conn); err != nil {
				_ = conn.Close()
				done <- err
				return
			}
			_ = conn.Close()
		}
		done <- nil
	}()
	t.Cleanup(func() {
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("fake logs RPC server: %v", err)
			}
		case <-time.After(5 * time.Second):
			t.Errorf("fake logs RPC server did not finish")
		}
	})
	return socket
}

func handleLogsFallbackConn(t *testing.T, conn net.Conn) error {
	t.Helper()
	dec := json.NewDecoder(conn)
	enc := json.NewEncoder(conn)

	var hs protocol.Envelope
	if err := dec.Decode(&hs); err != nil {
		return err
	}
	if err := enc.Encode(protocol.SuccessResponse(hs.ID, protocol.HandshakeResult{
		Accepted:          true,
		NegotiatedVersion: protocol.Version,
	})); err != nil {
		return err
	}

	var req protocol.Envelope
	if err := dec.Decode(&req); err != nil {
		return err
	}
	switch req.Method {
	case "agent.logs.stream":
		return enc.Encode(protocol.ErrorResponse(req.ID, protocol.ErrMethodNotFound, "method not found: agent.logs.stream", nil))
	case "agent.logs":
		var params protocol.AgentLogsParams
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return err
		}
		if params.Name != "alice" || params.Tail != 10 {
			t.Fatalf("params=%#v want name=alice tail=10", params)
		}
		return enc.Encode(protocol.SuccessResponse(req.ID, protocol.AgentLogsResult{Lines: []string{"fallback line"}}))
	default:
		t.Fatalf("unexpected method %q", req.Method)
		return nil
	}
}
