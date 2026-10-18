// Package adapter holds trusted HTTP clients for the fraud and payment
// services. Only these clients may produce screening decisions and settled
// payment evidence; request JSON is never trusted for either.
package adapter

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const maxBody = 1 << 20

// StatusError reports a non-2xx downstream response.
type StatusError struct {
	Method string
	Path   string
	Code   int
}

func (e *StatusError) Error() string {
	return fmt.Sprintf("%s %s: downstream status %d", e.Method, e.Path, e.Code)
}

func newClient(c *http.Client) *http.Client {
	if c != nil {
		return c
	}
	return &http.Client{Timeout: 5 * time.Second}
}

func call(ctx context.Context, c *http.Client, base, method, path string, in, out any) error {
	var body io.Reader
	if in != nil {
		raw, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(base, "/")+path, body)
	if err != nil {
		return err
	}
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := c.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(res.Body, maxBody+1))
	if err != nil {
		return err
	}
	if res.StatusCode < 200 || res.StatusCode > 299 {
		return &StatusError{Method: method, Path: path, Code: res.StatusCode}
	}
	if len(raw) > maxBody {
		return errors.New("downstream response too large")
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(raw, out)
}
