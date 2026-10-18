package command

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/TuanKietTran/loan-platform-go/internal/domain"
	"github.com/TuanKietTran/loan-platform-go/internal/repository"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// ApplicationScreen must replay eventID deterministically. Service never treats
// an unavailable or unknown fraud decision as approval.
type ApplicationScreen interface {
	Screen(context.Context, string, domain.Application) (string, error)
}

type AgreementService struct {
	Store  repository.AgreementRepository
	Screen ApplicationScreen
	NewID  func() uuid.UUID
}

func NewAgreementService(store repository.AgreementRepository, screen ApplicationScreen, newID func() uuid.UUID) *AgreementService {
	return &AgreementService{Store: store, Screen: screen, NewID: newID}
}
func commandKey(key string) error {
	if strings.TrimSpace(key) == "" || len(key) > 200 {
		return repository.ErrInvalidKey
	}
	return nil
}
func fingerprint(value any) (string, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}
func (s *AgreementService) ApplyApplication(ctx context.Context, key string, app domain.Application) (domain.Agreement, error) {
	if err := commandKey(key); err != nil {
		return domain.Agreement{}, err
	}
	if err := ctx.Err(); err != nil {
		return domain.Agreement{}, err
	}
	if err := app.Validate(); err != nil {
		return domain.Agreement{}, err
	}
	hash, err := fingerprint(app)
	if err != nil {
		return domain.Agreement{}, err
	}
	if prior, err := s.Store.FindApplication(ctx, key, hash); err == nil {
		return prior, nil
	} else if !errors.Is(err, repository.ErrNotFound) {
		return domain.Agreement{}, err
	}
	if s.Screen == nil || s.NewID == nil {
		return domain.Agreement{}, errors.New("application dependencies required")
	}
	// Stable namespaced event identity makes concurrent/resumed scoring replayable.
	event := uuid.NewSHA1(uuid.NameSpaceURL, []byte("loans:application:"+key)).String()
	decision, err := s.Screen.Screen(ctx, event, app)
	if err != nil {
		return domain.Agreement{}, err
	}
	agreement, err := domain.Originate(s.NewID(), app, decision)
	if err != nil {
		return domain.Agreement{}, err
	}
	return s.Store.CreateAgreement(ctx, key, hash, agreement)
}

type servicingPayload struct {
	Operation  string
	Date       time.Time
	Evidence   domain.PaymentEvidence
	Prepayment bool
	Fee        decimal.Decimal
}

func (s *AgreementService) apply(ctx context.Context, id uuid.UUID, key string, p servicingPayload) (repository.AgreementResult, error) {
	if err := commandKey(key); err != nil {
		return repository.AgreementResult{}, err
	}
	hash, err := fingerprint(p)
	if err != nil {
		return repository.AgreementResult{}, err
	}
	return s.Store.Apply(ctx, id, key, hash, func(a *domain.Agreement) (domain.Allocation, error) {
		switch p.Operation {
		case "disburse":
			return domain.Allocation{}, a.Disburse(p.Evidence)
		case "repay":
			return a.Repay(p.Date, p.Evidence, p.Prepayment)
		case "accrue":
			return domain.Allocation{}, a.Accrue(p.Date)
		case "assess":
			return domain.Allocation{}, a.Assess(p.Date, p.Fee)
		default:
			return domain.Allocation{}, errors.New("unsupported servicing operation")
		}
	})
}
func (s *AgreementService) Disburse(ctx context.Context, id uuid.UUID, key string, p domain.PaymentEvidence) (repository.AgreementResult, error) {
	return s.apply(ctx, id, key, servicingPayload{Operation: "disburse", Evidence: p})
}
func (s *AgreementService) Repay(ctx context.Context, id uuid.UUID, key string, date time.Time, p domain.PaymentEvidence, prepayment bool) (repository.AgreementResult, error) {
	return s.apply(ctx, id, key, servicingPayload{Operation: "repay", Date: date, Evidence: p, Prepayment: prepayment})
}
func (s *AgreementService) Accrue(ctx context.Context, id uuid.UUID, key string, date time.Time) (repository.AgreementResult, error) {
	return s.apply(ctx, id, key, servicingPayload{Operation: "accrue", Date: date})
}
func (s *AgreementService) Assess(ctx context.Context, id uuid.UUID, key string, date time.Time, fee decimal.Decimal) (repository.AgreementResult, error) {
	return s.apply(ctx, id, key, servicingPayload{Operation: "assess", Date: date, Fee: fee})
}
