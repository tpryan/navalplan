package sessions

import (
	"context"
	"testing"

	"google.golang.org/adk/v2/session"
)

func TestAutoCreate_Get_CreatesMissingSession(t *testing.T) {
	svc := NewAutoCreate(session.InMemoryService())
	ctx := context.Background()

	req := &session.GetRequest{
		AppName:   "app",
		UserID:    "user",
		SessionID: "does-not-exist-yet",
	}

	// A plain InMemoryService would return an error here since the session
	// was never created. AutoCreate should transparently create it and
	// return it instead.
	resp, err := svc.Get(ctx, req)
	if err != nil {
		t.Fatalf("Get() returned error, want auto-create to succeed: %v", err)
	}
	if resp.Session == nil {
		t.Fatal("Get() returned a nil session")
	}
	if resp.Session.ID() != req.SessionID {
		t.Errorf("Session.ID() = %q, want %q", resp.Session.ID(), req.SessionID)
	}
	if resp.Session.AppName() != req.AppName {
		t.Errorf("Session.AppName() = %q, want %q", resp.Session.AppName(), req.AppName)
	}
	if resp.Session.UserID() != req.UserID {
		t.Errorf("Session.UserID() = %q, want %q", resp.Session.UserID(), req.UserID)
	}
}

func TestAutoCreate_Get_ReturnsExistingSession(t *testing.T) {
	inner := session.InMemoryService()
	svc := NewAutoCreate(inner)
	ctx := context.Background()

	createReq := &session.CreateRequest{
		AppName:   "app",
		UserID:    "user",
		SessionID: "already-exists",
	}
	if _, err := inner.Create(ctx, createReq); err != nil {
		t.Fatalf("failed to seed session: %v", err)
	}

	resp, err := svc.Get(ctx, &session.GetRequest{
		AppName:   createReq.AppName,
		UserID:    createReq.UserID,
		SessionID: createReq.SessionID,
	})
	if err != nil {
		t.Fatalf("Get() returned error for an existing session: %v", err)
	}
	if resp.Session.ID() != createReq.SessionID {
		t.Errorf("Session.ID() = %q, want %q", resp.Session.ID(), createReq.SessionID)
	}
}

func TestAutoCreate_Get_TwiceIsIdempotent(t *testing.T) {
	svc := NewAutoCreate(session.InMemoryService())
	ctx := context.Background()
	req := &session.GetRequest{AppName: "app", UserID: "user", SessionID: "repeat"}

	first, err := svc.Get(ctx, req)
	if err != nil {
		t.Fatalf("first Get() failed: %v", err)
	}

	second, err := svc.Get(ctx, req)
	if err != nil {
		t.Fatalf("second Get() failed: %v", err)
	}

	if first.Session.ID() != second.Session.ID() {
		t.Errorf("expected the same session on repeated Get(), got IDs %q and %q", first.Session.ID(), second.Session.ID())
	}
}
