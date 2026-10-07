package platega

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestAPIErrorMessageAndHeaders(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Request-Id", "req-123")
		w.Header().Set("Retry-After", "7")
		writeJSON(w, 429, `{"message":"slow down","code":42}`)
	})
	_, err := c.GetBalances(context.Background())
	var ae *APIError
	if !errors.As(err, &ae) {
		t.Fatalf("expected APIError, got %v", err)
	}
	if ae.Message != "slow down" || ae.StatusCode != 429 || ae.RetryAfter != 7*time.Second ||
		ae.Header.Get("X-Request-Id") != "req-123" || !strings.Contains(string(ae.Body), `"code":42`) {
		t.Errorf("unexpected error: %+v", ae)
	}
	if !strings.Contains(err.Error(), "slow down") {
		t.Errorf("error text should carry the message: %v", err)
	}
}

func TestAPIErrorWithoutJSONBody(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(502)
		_, _ = w.Write([]byte("<html>bad gateway</html>"))
	})
	_, err := c.GetBalances(context.Background())
	var ae *APIError
	if !errors.As(err, &ae) || ae.Message != "" || !strings.Contains(err.Error(), "bad gateway") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestErrorCategories(t *testing.T) {
	cases := []struct {
		status int
		want   error
		others []error
	}{
		{400, ErrBadRequest, []error{ErrNotFound, ErrServer}},
		{401, ErrUnauthorized, []error{ErrNotFound, ErrServer}},
		{404, ErrNotFound, []error{ErrUnauthorized, ErrServer}},
		{429, ErrRateLimited, []error{ErrServer, ErrNotFound}},
		{500, ErrServer, []error{ErrRateLimited, ErrNotFound}},
		{503, ErrServer, []error{ErrRateLimited}},
	}
	for _, tc := range cases {
		err := error(&APIError{StatusCode: tc.status})
		if !errors.Is(err, tc.want) {
			t.Errorf("%d must match %v", tc.status, tc.want)
		}
		for _, o := range tc.others {
			if errors.Is(err, o) {
				t.Errorf("%d must not match %v", tc.status, o)
			}
		}
	}
	// Wrapped errors keep working.
	wrapped := errors.Join(errors.New("context"), &APIError{StatusCode: 404})
	if !IsNotFound(wrapped) || IsUnauthorized(wrapped) || IsBadRequest(wrapped) {
		t.Error("helpers must see through wrapping")
	}
}

func TestParseRetryAfter(t *testing.T) {
	now := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		in   string
		want time.Duration
		ok   bool
	}{
		{"", 0, false},
		{"abc", 0, false},
		{"-5", 0, false},
		{"0", 0, true},
		{"12", 12 * time.Second, true},
		{now.Add(90 * time.Second).Format(http.TimeFormat), 90 * time.Second, true},
		{now.Add(-time.Hour).Format(http.TimeFormat), 0, true}, // a date in the past
	}
	for _, tc := range cases {
		got, ok := parseRetryAfter(tc.in, now)
		if got != tc.want || ok != tc.ok {
			t.Errorf("parseRetryAfter(%q) = %v, %v; want %v, %v", tc.in, got, ok, tc.want, tc.ok)
		}
	}
}

func TestRetryDelay(t *testing.T) {
	now := time.Now()
	// Jitter stays within [backoff/2, backoff].
	for range 200 {
		d := retryDelay(time.Second, http.Header{}, now)
		if d < 500*time.Millisecond || d > time.Second {
			t.Fatalf("jittered delay out of range: %v", d)
		}
	}
	// A zero backoff must not panic.
	if d := retryDelay(0, http.Header{}, now); d != 0 {
		t.Errorf("zero backoff = %v", d)
	}
	// Retry-After wins over backoff, but is capped.
	h := http.Header{"Retry-After": {"3"}}
	if d := retryDelay(time.Hour, h, now); d != 3*time.Second {
		t.Errorf("Retry-After ignored: %v", d)
	}
	h = http.Header{"Retry-After": {"86400"}}
	if d := retryDelay(time.Second, h, now); d != maxRetryWait {
		t.Errorf("Retry-After not capped: %v", d)
	}
}

func TestRetryHonoursRetryAfterHeader(t *testing.T) {
	var calls int
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			w.Header().Set("Retry-After", "0")
			writeJSON(w, 429, `{"message":"rate limited"}`)
			return
		}
		writeJSON(w, 200, `[]`)
	}, WithRetry(2, time.Hour)) // the hour must be ignored: Retry-After says 0
	done := make(chan error, 1)
	go func() { _, err := c.GetBalances(context.Background()); done <- err }()
	select {
	case err := <-done:
		if err != nil || calls != 2 {
			t.Fatalf("err=%v calls=%d", err, calls)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Retry-After: 0 was not honoured")
	}
}
