package repository

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/TuanKietTran/loan-platform-go/internal/domain"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

func storedAgreement(t *testing.T, m *MemoryAgreements) domain.Agreement {
	t.Helper()
	a := domain.Agreement{ID: uuid.New(), Status: domain.StatusPending, Schedule: domain.Schedule{Instalments: []domain.Instalment{{Number: 1}}}}
	got, err := m.CreateAgreement(context.Background(), "application", "payload", a)
	if err != nil {
		t.Fatal(err)
	}
	return got
}
func TestMemoryAgreementConcurrentCreateAndMutationReplay(t *testing.T) {
	ctx := context.Background()
	m := NewMemoryAgreements()
	a := storedAgreement(t, m)
	var wg sync.WaitGroup
	for range 40 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got, err := m.CreateAgreement(ctx, "application", "payload", domain.Agreement{ID: uuid.New()})
			if err != nil || got.ID != a.ID {
				t.Errorf("create replay %v %v", got.ID, err)
			}
			result, err := m.Apply(ctx, a.ID, "activate", "activate", func(next *domain.Agreement) (domain.Allocation, error) {
				next.Status = domain.StatusActive
				next.Outstanding = next.Outstanding.Add(decimal.NewFromInt(1))
				return domain.Allocation{}, nil
			})
			if err != nil || !result.Agreement.Outstanding.Equal(decimal.NewFromInt(1)) {
				t.Errorf("mutation replay %v", err)
			}
		}()
	}
	wg.Wait()
	result, err := m.Apply(ctx, a.ID, "next", "next", func(next *domain.Agreement) (domain.Allocation, error) {
		next.Status = domain.StatusDelinquent
		return domain.Allocation{}, nil
	})
	if err != nil || result.Agreement.Status != domain.StatusDelinquent {
		t.Fatal(err)
	}
	replay, err := m.Apply(ctx, a.ID, "activate", "activate", nil)
	if err != nil || replay.Agreement.Status != domain.StatusActive {
		t.Fatal("not original snapshot", err)
	}
	replay.Agreement.Schedule.Instalments[0].Number = 99
	got, _ := m.GetAgreement(ctx, a.ID)
	if got.Schedule.Instalments[0].Number != 1 || got.Status != domain.StatusDelinquent {
		t.Fatal("result aliases stored state")
	}
	original, err := m.FindApplication(ctx, "application", "payload")
	if err != nil || original.Status != domain.StatusPending {
		t.Fatal("application replay advanced")
	}
	original.Schedule.Instalments[0].Number = 88
	original, _ = m.FindApplication(ctx, "application", "payload")
	if original.Schedule.Instalments[0].Number != 1 {
		t.Fatal("application snapshot aliases")
	}
	if _, err = m.CreateAgreement(ctx, "application", "changed", a); !errors.Is(err, ErrConflict) {
		t.Fatal(err)
	}
	if _, err = m.CreateAgreement(ctx, "other", "other", a); !errors.Is(err, ErrConflict) {
		t.Fatal("ID overwritten", err)
	}
	if _, err = m.FindApplication(ctx, "application", "changed"); !errors.Is(err, ErrConflict) {
		t.Fatal(err)
	}
	if _, err = m.Apply(ctx, a.ID, "activate", "changed", nil); !errors.Is(err, ErrConflict) {
		t.Fatal(err)
	}
}
func TestMemoryAgreementRollbackCancellationAndValidation(t *testing.T) {
	ctx := context.Background()
	m := NewMemoryAgreements()
	a := storedAgreement(t, m)
	fail := errors.New("failed mutation")
	if _, err := m.Apply(ctx, a.ID, "mutation", "fingerprint", func(next *domain.Agreement) (domain.Allocation, error) {
		next.Status = domain.StatusActive
		return domain.Allocation{}, fail
	}); !errors.Is(err, fail) {
		t.Fatal(err)
	}
	got, _ := m.GetAgreement(ctx, a.ID)
	if got.Status != domain.StatusPending {
		t.Fatal("failed mutation committed")
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := m.FindApplication(canceled, "application", "payload"); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := m.CreateAgreement(canceled, "key", "hash", a); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := m.GetAgreement(canceled, a.ID); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := m.Apply(canceled, a.ID, "key", "hash", nil); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	during, cancelDuring := context.WithCancel(ctx)
	if _, err := m.Apply(during, a.ID, "during", "hash", func(next *domain.Agreement) (domain.Allocation, error) {
		next.Status = domain.StatusActive
		cancelDuring()
		return domain.Allocation{}, nil
	}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := m.Apply(ctx, a.ID, "identity", "hash", func(next *domain.Agreement) (domain.Allocation, error) {
		next.ID = uuid.New()
		return domain.Allocation{}, nil
	}); err == nil {
		t.Fatal("changed ID")
	}
	got, _ = m.GetAgreement(ctx, a.ID)
	if got.Status != domain.StatusPending {
		t.Fatal("rollback lost")
	}
	if _, err := m.Apply(ctx, uuid.New(), "key", "hash", nil); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if _, err := m.GetAgreement(ctx, uuid.New()); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if _, err := m.FindApplication(ctx, "missing", "hash"); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	for _, identity := range [][2]string{{"", "hash"}, {"key", ""}} {
		if _, err := m.FindApplication(ctx, identity[0], identity[1]); !errors.Is(err, ErrInvalidKey) {
			t.Fatal(err)
		}
		if _, err := m.CreateAgreement(ctx, identity[0], identity[1], a); !errors.Is(err, ErrInvalidKey) {
			t.Fatal(err)
		}
		if _, err := m.Apply(ctx, a.ID, identity[0], identity[1], nil); !errors.Is(err, ErrInvalidKey) {
			t.Fatal(err)
		}
	}
	if _, err := m.CreateAgreement(ctx, "nil", "hash", domain.Agreement{}); err == nil {
		t.Fatal("nil ID")
	}
}
