// Package app wires the loan service: configuration, trusted downstream
// adapters, lifecycle routes, readiness, recovery and graceful shutdown.
package app

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/TuanKietTran/loan-platform-go/internal/adapter"
	"github.com/TuanKietTran/loan-platform-go/internal/api"
	"github.com/TuanKietTran/loan-platform-go/internal/command"
	"github.com/TuanKietTran/loan-platform-go/internal/query"
	"github.com/TuanKietTran/loan-platform-go/internal/repository"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"
)

type Config struct {
	Addr         string
	FraudURL     string
	PaymentsURL  string
	RecoverEvery time.Duration
}

// LoadConfig reads ADDR (default :8084), FRAUD_URL, PAYMENTS_URL (both
// required absolute http(s) URLs) and RECOVER_INTERVAL (default 30s). URL
// values are never echoed in errors.
func LoadConfig(getenv func(string) string) (Config, error) {
	c := Config{Addr: getenv("ADDR"), FraudURL: getenv("FRAUD_URL"), PaymentsURL: getenv("PAYMENTS_URL"), RecoverEvery: 30 * time.Second}
	if c.Addr == "" {
		c.Addr = ":8084"
	}
	var errs []error
	if v := getenv("RECOVER_INTERVAL"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil || d <= 0 {
			errs = append(errs, errors.New("RECOVER_INTERVAL must be a positive duration"))
		} else {
			c.RecoverEvery = d
		}
	}
	for name, v := range map[string]string{"FRAUD_URL": c.FraudURL, "PAYMENTS_URL": c.PaymentsURL} {
		if u, err := url.Parse(v); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			errs = append(errs, fmt.Errorf("%s must be an absolute http(s) URL", name))
		}
	}
	return c, errors.Join(errs...)
}

type App struct {
	Handler http.Handler
	Cash    *command.CashService
	// Ready reports whether downstream services are reachable.
	Ready func(context.Context) error
	cfg   Config
}

func resilientClient() *http.Client {
	return &http.Client{Transport: &adapter.Resilient{}, Timeout: 10 * time.Second}
}

// probe treats any non-5xx answer from GET /healthz as reachable.
func probe(ctx context.Context, c *http.Client, base string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(base, "/")+"/healthz", nil)
	if err != nil {
		return err
	}
	res, err := c.Do(req)
	if err != nil {
		return err
	}
	res.Body.Close()
	if res.StatusCode >= 500 {
		return fmt.Errorf("status %d", res.StatusCode)
	}
	return nil
}

func New(cfg Config) *App {
	a := &App{cfg: cfg}
	plain := &http.Client{Timeout: 2 * time.Second}
	a.Ready = func(ctx context.Context) error {
		var errs []error
		if probe(ctx, plain, cfg.FraudURL) != nil {
			errs = append(errs, errors.New("fraud service not ready"))
		}
		if probe(ctx, plain, cfg.PaymentsURL) != nil {
			errs = append(errs, errors.New("payments service not ready"))
		}
		return errors.Join(errs...)
	}

	agreements := command.NewAgreementService(repository.NewMemoryAgreements(), adapter.NewFraud(cfg.FraudURL, resilientClient()), uuid.New)
	a.Cash = command.NewCashService(agreements, repository.NewMemoryIntents(), adapter.NewPayment(cfg.PaymentsURL, resilientClient()))

	legacy := repository.NewInMemoryLoan()
	v1 := api.NewHandlers(command.NewCreateLoanHandler(legacy), query.NewGetPayoffHandler(legacy)).Routes()
	(&api.Lifecycle{Agreements: agreements, Cash: a.Cash}).Register(v1)

	r := chi.NewRouter()
	r.Use(middleware.Logger, middleware.Recoverer)
	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}` + "\n"))
	})
	r.Get("/readyz", func(w http.ResponseWriter, req *http.Request) {
		if err := a.Ready(req.Context()); err != nil {
			api.WriteError(w, http.StatusServiceUnavailable, "not_ready", err.Error())
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ready"}` + "\n"))
	})
	r.Mount("/v1", v1)
	a.Handler = r
	return a
}

// RecoverLoop re-runs unfinished cash intents now and every interval until
// ctx ends. Payment keys are stable, so reruns never move money twice.
func RecoverLoop(ctx context.Context, cash *command.CashService, every time.Duration, logf func(string, ...any)) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		if n, err := cash.Recover(ctx); err != nil && ctx.Err() == nil {
			logf("cash recovery: completed=%d err=%v", n, err)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// Serve runs the API and recovery loop until ctx ends, then drains requests.
func Serve(ctx context.Context, ln net.Listener, a *App) error {
	srv := &http.Server{
		Handler:           a.Handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      45 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    1 << 16,
	}
	loopCtx, stop := context.WithCancel(ctx)
	defer stop()
	go RecoverLoop(loopCtx, a.Cash, a.cfg.RecoverEvery, log.Printf)
	errc := make(chan error, 1)
	go func() { errc <- srv.Serve(ln) }()
	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}
	shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return srv.Shutdown(shutdown)
}

func Run(ctx context.Context, cfg Config) error {
	ln, err := net.Listen("tcp", cfg.Addr)
	if err != nil {
		return err
	}
	log.Printf("Loan API listening on %s", ln.Addr())
	return Serve(ctx, ln, New(cfg))
}
