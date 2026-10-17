package api

import (
	"github.com/TuanKietTran/loan-platform-go/internal/command"
	"github.com/TuanKietTran/loan-platform-go/internal/query"
	"github.com/TuanKietTran/loan-platform-go/internal/repository"
	"github.com/go-chi/chi/v5"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCreateRequiresKeyReplayAndConflict(t *testing.T) {
	repo := repository.NewInMemoryLoan()
	h := NewHandlers(command.NewCreateLoanHandler(repo), query.NewGetPayoffHandler(repo))
	r := chi.NewRouter()
	r.Mount("/v1", h.Routes())
	srv := httptest.NewServer(r)
	defer srv.Close()
	post := func(body string) (int, string) {
		res, e := http.Post(srv.URL+"/v1/loans", "application/json", strings.NewReader(body))
		if e != nil {
			t.Fatal(e)
		}
		defer res.Body.Close()
		b := make([]byte, 1024)
		n, _ := res.Body.Read(b)
		return res.StatusCode, string(b[:n])
	}
	body := `{"idempotency_key":"key-a","principal":"100.00","interest_rate":"0.1","term_months":12}`
	s1, b1 := post(body)
	s2, b2 := post(body)
	if s1 != 200 || s2 != 200 || b1 != b2 {
		t.Fatalf("replay: %d %s %d %s", s1, b1, s2, b2)
	}
	s3, _ := post(`{"idempotency_key":"key-a","principal":"101.00","interest_rate":"0.1","term_months":12}`)
	if s3 != http.StatusConflict {
		t.Fatalf("changed payload status %d", s3)
	}
	s4, _ := post(`{"principal":"100","interest_rate":"0.1","term_months":12}`)
	if s4 != http.StatusBadRequest {
		t.Fatalf("missing key status %d", s4)
	}
}
