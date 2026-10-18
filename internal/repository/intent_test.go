package repository

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
)

func TestMemoryIntents(t *testing.T) {
	ctx := context.Background()
	m := NewMemoryIntents()
	id := uuid.New()
	in := CashIntent{AgreementID: id, Key: "k", Fingerprint: "f", Operation: "disburse", Done: true}
	calls := 0
	validate := func() error { calls++; return nil }
	got, err := m.Begin(ctx, in, validate)
	if err != nil || got.Done || calls != 1 {
		t.Fatal(got, err, calls)
	}
	if _, err := m.Begin(ctx, in, validate); err != nil || calls != 1 {
		t.Fatal("replay must not revalidate", err, calls)
	}
	in.Fingerprint = "other"
	if _, err := m.Begin(ctx, in, nil); !errors.Is(err, ErrConflict) {
		t.Fatal(err)
	}
	bad := errors.New("invalid")
	if _, err := m.Begin(ctx, CashIntent{AgreementID: id, Key: "z", Fingerprint: "f"}, func() error { return bad }); !errors.Is(err, bad) {
		t.Fatal(err)
	}
	if p, _ := m.Pending(ctx); len(p) != 1 {
		t.Fatal("rejected intent stored", p)
	}
	if _, err := m.Begin(ctx, CashIntent{AgreementID: id}, nil); !errors.Is(err, ErrInvalidKey) {
		t.Fatal(err)
	}
	if err := m.Complete(ctx, id, "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if err := m.Complete(ctx, id, "k"); err != nil {
		t.Fatal(err)
	}
	if p, _ := m.Pending(ctx); len(p) != 0 {
		t.Fatal(p)
	}
	cctx, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := m.Begin(cctx, CashIntent{AgreementID: id, Key: "c", Fingerprint: "f"}, nil); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if err := m.Complete(cctx, id, "k"); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := m.Pending(cctx); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
