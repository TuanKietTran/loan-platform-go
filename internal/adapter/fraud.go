package adapter

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/TuanKietTran/loan-platform-go/internal/domain"
)

// Fraud scores applications through POST /v1/score. Any transport, status,
// identity or vocabulary problem is an error so callers fail closed.
type Fraud struct {
	BaseURL string
	HTTP    *http.Client
}

func NewFraud(baseURL string, c *http.Client) *Fraud {
	return &Fraud{BaseURL: baseURL, HTTP: newClient(c)}
}

type scoreRequest struct {
	EventID  string `json:"event_id"`
	Account  string `json:"account"`
	Amount   string `json:"amount"`
	Currency string `json:"currency"`
}

type scoreResponse struct {
	EventID  string `json:"event_id"`
	Decision string `json:"decision"`
}

func (f *Fraud) Screen(ctx context.Context, eventID string, app domain.Application) (string, error) {
	if eventID == "" {
		return "", errors.New("fraud event ID required")
	}
	if app.Terms.Currency != "USD" {
		return "", errors.New("fraud screening supports USD only")
	}
	var out scoreResponse
	err := call(ctx, newClient(f.HTTP), f.BaseURL, http.MethodPost, "/v1/score", scoreRequest{
		EventID:  eventID,
		Account:  app.BorrowerAccount.String(),
		Amount:   app.Terms.Principal.StringFixed(2),
		Currency: "USD",
	}, &out)
	if err != nil {
		return "", err
	}
	if out.EventID != eventID {
		return "", errors.New("fraud response event mismatch")
	}
	switch out.Decision {
	case "ALLOW", "REVIEW", "DENY", "BLOCK":
		return out.Decision, nil
	}
	return "", fmt.Errorf("unknown fraud decision %q", out.Decision)
}
