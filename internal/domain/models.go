package domain

import (
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

type LoanID = uuid.UUID

type LoanStatus string

const (
	StatusPending  LoanStatus = "PENDING"
	StatusActive   LoanStatus = "ACTIVE"
	StatusPaidOff  LoanStatus = "PAID_OFF"
)

type Loan struct {
	ID                  LoanID
	Principal           decimal.Decimal
	InterestRate        decimal.Decimal
	TermMonths          int
	Status              LoanStatus
	OutstandingPrincipal decimal.Decimal
	AccruedInterest     decimal.Decimal
}
