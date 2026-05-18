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

func TestChatMessageIDDeterministic(t *testing.T) {
	id1 := chatMessageID("alice", "", "hello world")
	id2 := chatMessageID("alice", "", "hello world")
	if id1 != id2 {
		t.Fatalf("expected stable ID, got %q vs %q", id1, id2)
	}
	id3 := chatMessageID("alice", "", "different content")
	if id1 == id3 {
		t.Fatal("expected different IDs for different content")
	}
	id4 := chatMessageID("bob", "", "hello world")
	if id1 == id4 {
		t.Fatal("expected different IDs for different agent names")
	}
	id5 := chatMessageID("alice", "parentXYZ", "hello world")
	if id1 == id5 {
		t.Fatal("expected different IDs for different parent IDs")
	}
	// Length must be 64 hex chars (sha256 = 32 bytes = 64 hex).
	if len(id1) != 64 {
		t.Fatalf("expected 64-char hex ID, got length %d: %q", len(id1), id1)
	}
}

func TestChatHistoryListRoundTrip(t *testing.T) {
	socket := serveOneRPC(t, func(t *testing.T, req protocol.Envelope) *protocol.Envelope {
		if req.Method != "agent.chat.history.list" {
			t.Fatalf("method=%q want agent.chat.history.list", req.Method)
		}
		var params protocol.ChatHistoryListParams
		if err := json.Unmarshal(req.Params, &params); err != nil {
			t.Fatalf("unmarshal params: %v", err)
		}
		if params.Name != "alice" || params.Limit != 25 {
			t.Fatalf("params=%#v want name=alice limit=25", params)
		}
		return protocol.SuccessResponse(req.ID, protocol.ChatHistoryListResult{Messages: []protocol.ChatMessage{
			{Role: "user", Content: "hello", MessageID: "m1", Timestamp: 123},
			{Role: "agent", Content: "hi", ParentID: "m1", MessageID: "m2", Timestamp: 124},
		}})
	})

	c := NewRPCClient(socket)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	messages, err := c.ChatHistoryList(ctx, "alice", 25)
	if err != nil {
		t.Fatalf("ChatHistoryList: %v", err)
	}
	if len(messages) != 2 {
		t.Fatalf("messages len=%d want 2", len(messages))
	}
	if messages[0].MessageID != "m1" || messages[1].ParentID != "m1" {
		t.Fatalf("unexpected messages: %#v", messages)
	}
}

func serveOneRPC(t *testing.T, handle func(*testing.T, protocol.Envelope) *protocol.Envelope) string {
	t.Helper()
	socket := filepath.Join(t.TempDir(), "dclaw.sock")
	ln, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatalf("listen unix: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })

	done := make(chan error, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			done <- err
			return
		}
		defer conn.Close()
		dec := json.NewDecoder(conn)
		enc := json.NewEncoder(conn)

		var hs protocol.Envelope
		if err := dec.Decode(&hs); err != nil {
			done <- err
			return
		}
		if err := enc.Encode(protocol.SuccessResponse(hs.ID, protocol.HandshakeResult{
			Accepted:          true,
			NegotiatedVersion: protocol.Version,
		})); err != nil {
			done <- err
			return
		}

		var req protocol.Envelope
		if err := dec.Decode(&req); err != nil {
			done <- err
			return
		}
		if err := enc.Encode(handle(t, req)); err != nil {
			done <- err
			return
		}
		done <- nil
	}()
	t.Cleanup(func() {
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("fake RPC server: %v", err)
			}
		case <-time.After(5 * time.Second):
			t.Errorf("fake RPC server did not finish")
		}
	})
	return socket
}
