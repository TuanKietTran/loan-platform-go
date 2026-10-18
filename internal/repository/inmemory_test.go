package repository

import (
	"context"
	"errors"
	"testing"

	"github.com/TuanKietTran/loan-platform-go/internal/domain"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

func TestLegacyMemoryCreateReplayAccrualAndIsolation(t *testing.T) {
	ctx := context.Background()
	store := NewInMemoryLoan()
	loan := domain.Loan{Principal: decimal.NewFromInt(1000), OutstandingPrincipal: decimal.NewFromInt(1000), InterestRate: decimal.RequireFromString("0.365"), TermMonths: 12, IdempotencyKey: "key"}
	if err := store.Save(ctx, loan); err != nil {
		t.Fatal(err)
	}
	got, ok := store.GetByKey("key")
	if !ok || got.ID == uuid.Nil {
		t.Fatal("missing generated ID")
	}
	id := got.ID
	got.TermMonths = 99
	got, err := store.GetByID(ctx, id)
	if err != nil || got.TermMonths != 12 {
		t.Fatal("returned mutable state")
	}
	if err = store.AccrueInterest(ctx, id, 2); err != nil {
		t.Fatal(err)
	}
	got, _ = store.GetByID(ctx, id)
	if !got.AccruedInterest.Equal(decimal.NewFromInt(2)) {
		t.Fatal(got.AccruedInterest)
	}
	if err = store.Save(ctx, loan); err != nil {
		t.Fatal(err)
	}
	got, _ = store.GetByID(ctx, id)
	if !got.AccruedInterest.Equal(decimal.NewFromInt(2)) {
		t.Fatal("create replay reset accrual")
	}
	for _, change := range []func(*domain.Loan){func(l *domain.Loan) { l.Principal = decimal.NewFromInt(1) }, func(l *domain.Loan) { l.InterestRate = decimal.Zero }, func(l *domain.Loan) { l.TermMonths = 1 }} {
		different := loan
		change(&different)
		if err = store.Save(ctx, different); !errors.Is(err, ErrConflict) {
			t.Fatal(err)
		}
	}
	if _, ok = store.GetByKey("missing"); ok {
		t.Fatal("unexpected key")
	}
	if _, err = store.GetByID(ctx, uuid.New()); err == nil {
		t.Fatal("missing ID")
	}
	if err = store.AccrueInterest(ctx, uuid.New(), 1); err == nil {
		t.Fatal("missing accrual ID")
	}
	if err = store.AccrueInterest(ctx, id, -1); err == nil {
		t.Fatal("negative accrual")
	}
	loan.ID = id
	loan.IdempotencyKey = "other"
	if err = store.Save(ctx, loan); !errors.Is(err, ErrConflict) {
		t.Fatal("overwrote identity", err)
	}
	if _, ok = store.GetByKey("other"); ok {
		t.Fatal("failed insert reserved key")
	}
	// Adapter-level unkeyed imports remain possible; public commands require keys.
	loan.ID = uuid.Nil
	loan.IdempotencyKey = ""
	if err = store.Save(ctx, loan); err != nil {
		t.Fatal(err)
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if err = store.Save(canceled, loan); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err = store.GetByID(canceled, id); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if err = store.AccrueInterest(canceled, id, 1); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
