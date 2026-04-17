package client

import "testing"

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
