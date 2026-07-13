package server

import "testing"

func TestConfirmRegistry(t *testing.T) {
	s := &server{confirms: make(map[string]chan bool)}

	// Resolving an unknown ID reports no pending confirmation.
	if s.resolveConfirm("missing", true) {
		t.Error("resolveConfirm on unknown id should return false")
	}

	ch := make(chan bool, 1)
	s.addConfirm("c1", ch)

	if !s.resolveConfirm("c1", true) {
		t.Fatal("resolveConfirm on registered id should return true")
	}
	select {
	case v := <-ch:
		if !v {
			t.Error("expected approved=true on the channel")
		}
	default:
		t.Error("decision was not delivered to the channel")
	}

	s.removeConfirm("c1")
	if s.resolveConfirm("c1", false) {
		t.Error("resolveConfirm after removeConfirm should return false")
	}
}
