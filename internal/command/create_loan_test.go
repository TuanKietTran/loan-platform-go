package command

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/TuanKietTran/loan-platform-go/internal/repository"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

func TestCreateLoanCanonicalIdentityAcrossHandlers(t *testing.T) {
	ctx := context.Background()
	store := repository.NewInMemoryLoan()
	command := CreateLoanCommand{Principal: decimal.NewFromInt(100), InterestRate: decimal.Zero, TermMonths: 12, IdempotencyKey: "shared"}
	var wg sync.WaitGroup
	var mu sync.Mutex
	ids := map[uuid.UUID]bool{}
	for range 50 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			id, err := NewCreateLoanHandler(store).Handle(ctx, command)
			if err != nil {
				t.Error(err)
				return
			}
			if _, err = store.GetByID(ctx, id); err != nil {
				t.Error("returned an unpersisted identity", err)
			}
			mu.Lock()
			ids[id] = true
			mu.Unlock()
		}()
	}
	wg.Wait()
	if len(ids) != 1 {
		t.Fatalf("returned %d identities", len(ids))
	}
	command.Principal = decimal.NewFromInt(101)
	if _, err := NewCreateLoanHandler(store).Handle(ctx, command); !errors.Is(err, repository.ErrConflict) {
		t.Fatal(err)
	}
}
func TestCreateLoanValidatesTermsAndPropagatesCancellation(t *testing.T) {
	store := repository.NewInMemoryLoan()
	handler := NewCreateLoanHandler(store)
	valid := CreateLoanCommand{Principal: decimal.NewFromInt(1), InterestRate: decimal.Zero, TermMonths: 1, IdempotencyKey: "key"}
	for _, change := range []func(*CreateLoanCommand){
		func(c *CreateLoanCommand) { c.IdempotencyKey = "" },
		func(c *CreateLoanCommand) { c.Principal = decimal.Zero },
		func(c *CreateLoanCommand) { c.Principal = decimal.NewFromInt(-1) },
		func(c *CreateLoanCommand) { c.InterestRate = decimal.NewFromInt(-1) },
		func(c *CreateLoanCommand) { c.TermMonths = 0 },
		func(c *CreateLoanCommand) { c.TermMonths = 601 },
	} {
		c := valid
		change(&c)
		if _, err := handler.Handle(context.Background(), c); err == nil {
			t.Fatal("invalid terms accepted")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := handler.Handle(ctx, valid); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
