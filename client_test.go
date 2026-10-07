package platega

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const (
	testMerchant = "00000000-0000-0000-0000-000000000001"
	testSecret   = "test-secret"
)

func newTestClient(t *testing.T, h http.HandlerFunc, opts ...Option) *Client {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c, err := New(testMerchant, testSecret, append([]Option{WithBaseURL(srv.URL)}, opts...)...)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func writeJSON(w http.ResponseWriter, status int, body string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = io.WriteString(w, body)
}

func readBody(t *testing.T, r *http.Request) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.NewDecoder(r.Body).Decode(&m); err != nil {
		t.Fatalf("decode request body: %v", err)
	}
	return m
}

func TestNewValidation(t *testing.T) {
	if _, err := New("", "x"); err == nil {
		t.Error("empty merchant ID must fail")
	}
	if _, err := New("x", ""); err == nil {
		t.Error("empty secret must fail")
	}
	if _, err := New("x", "y", WithBaseURL("not a url")); err == nil {
		t.Error("invalid base URL must fail")
	}
}

func TestAuthHeaders(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("X-MerchantId"); got != testMerchant {
			t.Errorf("X-MerchantId = %q", got)
		}
		if got := r.Header.Get("X-Secret"); got != testSecret {
			t.Errorf("X-Secret = %q", got)
		}
		if r.Header.Get("Authorization") != "" {
			t.Error("key auth must not send Authorization")
		}
		writeJSON(w, 200, `[]`)
	}, WithUserAgent("custom/1"))
	if _, err := c.GetBalances(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestCreatePayment(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/transaction/process" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		body := readBody(t, r)
		if body["paymentMethod"] != float64(2) {
			t.Errorf("paymentMethod = %v", body["paymentMethod"])
		}
		details := body["paymentDetails"].(map[string]any)
		if details["amount"] != float64(500) || details["currency"] != "RUB" {
			t.Errorf("paymentDetails = %v", details)
		}
		if body["return"] != "https://example.com/ok" || body["failedUrl"] != "https://example.com/fail" {
			t.Errorf("urls = %v / %v", body["return"], body["failedUrl"])
		}
		if _, ok := body["id"]; ok {
			t.Error("id must never be sent")
		}
		if md := body["metadata"].(map[string]any); md["userId"] != "42" {
			t.Errorf("metadata = %v", md)
		}
		writeJSON(w, 200, `{
			"paymentMethod":"SBPQR","transactionId":"3fa85f64-5717-4562-b3fc-2c463f66afa6",
			"redirect":"https://pay.example/?qrsbp","return":"https://example.com/ok",
			"paymentDetails":"100 RUB","status":"PENDING","expiresIn":"00:15:00",
			"merchantId":"m","usdtRate":93.45}`)
	})
	p, err := c.CreatePayment(context.Background(), CreatePaymentRequest{
		PaymentMethod: MethodSBPQR,
		PaymentParams: PaymentParams{
			Details:     PaymentDetails{Amount: MustAmount("500"), Currency: "RUB"},
			Description: "order",
			ReturnURL:   "https://example.com/ok",
			FailedURL:   "https://example.com/fail",
			Metadata:    &Metadata{UserID: "42"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if p.TransactionID != "3fa85f64-5717-4562-b3fc-2c463f66afa6" || p.Status != StatusPending ||
		p.Redirect != "https://pay.example/?qrsbp" || p.USDTRate.String() != "93.45" {
		t.Errorf("unexpected payment: %+v", p)
	}
	if p.PaymentDetails.Amount.String() != "100" || p.PaymentDetails.Currency != "RUB" {
		t.Errorf("string payment details not parsed: %+v", p.PaymentDetails)
	}
}

func TestCreatePaymentValidation(t *testing.T) {
	c, _ := New(testMerchant, testSecret, WithBaseURL("http://127.0.0.1:1"))
	ok := PaymentParams{Details: PaymentDetails{Amount: MustAmount("1"), Currency: "RUB"}}
	cases := map[string]CreatePaymentRequest{
		"no method":   {PaymentParams: ok},
		"zero amount": {PaymentMethod: MethodSBPQR, PaymentParams: PaymentParams{Details: PaymentDetails{Currency: "RUB"}}},
		"no currency": {PaymentMethod: MethodSBPQR, PaymentParams: PaymentParams{Details: PaymentDetails{Amount: MustAmount("1")}}},
	}
	for name, req := range cases {
		if _, err := c.CreatePayment(context.Background(), req); err == nil {
			t.Errorf("%s: expected validation error", name)
		}
	}
}

func TestCreatePaymentLink(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v2/transaction/process" {
			t.Errorf("path = %s", r.URL.Path)
		}
		if _, ok := readBody(t, r)["paymentMethod"]; ok {
			t.Error("payment link must not send paymentMethod")
		}
		writeJSON(w, 200, `{"transactionId":"t1","status":"PENDING","url":"https://pay.example/?id=t1","expiresIn":"00:15:00","rate":91.2}`)
	})
	l, err := c.CreatePaymentLink(context.Background(), CreatePaymentLinkRequest{
		PaymentParams: PaymentParams{Details: PaymentDetails{Amount: MustAmount("10"), Currency: "RUB"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if l.TransactionID != "t1" || l.URL != "https://pay.example/?id=t1" || l.Rate.String() != "91.2" {
		t.Errorf("unexpected link: %+v", l)
	}
}

func TestGetTransaction(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/transaction/abc":
			writeJSON(w, 200, `{"id":"abc","status":"CONFIRMED","paymentDetails":{"amount":2000,"currency":"RUB"},
				"mechantId":"m1","comission":1.5,"comissionUsdt":0.5,"amountUsdt":10.9,"paymentMethod":"SBPQR",
				"comissionType":1,"externalId":"e","description":"d","payload":"p"}`)
		default:
			writeJSON(w, 404, `{"error":"not found"}`)
		}
	})
	tx, err := c.GetTransaction(context.Background(), "abc")
	if err != nil {
		t.Fatal(err)
	}
	if tx.Status != StatusConfirmed || tx.MerchantID != "m1" || tx.Commission.String() != "1.5" ||
		tx.CommissionUSDT.String() != "0.5" || tx.PaymentDetails.Amount.String() != "2000" {
		t.Errorf("unexpected transaction: %+v", tx)
	}
	_, err = c.GetTransaction(context.Background(), "missing")
	if !IsNotFound(err) {
		t.Fatalf("expected not found, got %v", err)
	}
	var ae *APIError
	if !errors.As(err, &ae) || !strings.Contains(string(ae.Body), "not found") {
		t.Errorf("APIError body not preserved: %v", err)
	}
	if _, err := c.GetTransaction(context.Background(), ""); err == nil {
		t.Error("empty id must fail")
	}
}

func TestPathIDIsEscaped(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.EscapedPath() != "/transaction/a%2Fb%3Fc" {
			t.Errorf("escaped path = %s", r.URL.EscapedPath())
		}
		writeJSON(w, 200, `{}`)
	})
	if _, err := c.GetTransaction(context.Background(), "a/b?c"); err != nil {
		t.Fatal(err)
	}
}

func TestUnauthorized(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 401, ``) })
	_, err := c.GetBalances(context.Background())
	if !IsUnauthorized(err) {
		t.Fatalf("expected unauthorized, got %v", err)
	}
	if !strings.Contains(err.Error(), "HTTP 401") {
		t.Errorf("error text: %v", err)
	}
}

func TestBalancesAndH2H(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/balance/all":
			writeJSON(w, 200, `[{"amount":15000.5,"currency":"RUB"},{"amount":200,"currency":"USDT","frozenBalance":500}]`)
		case "/h2h/tx1":
			writeJSON(w, 200, `{"amount":136.12,"qr":"https://qr.example/x"}`)
		}
	})
	b, err := c.GetBalances(context.Background())
	if err != nil || len(b) != 2 || b[1].Currency != "USDT" || b[1].FrozenBalance.String() != "500" {
		t.Fatalf("balances: %+v, %v", b, err)
	}
	q, err := c.GetH2HQR(context.Background(), "tx1")
	if err != nil || q.Amount.String() != "136.12" || q.QR != "https://qr.example/x" {
		t.Fatalf("h2h: %+v, %v", q, err)
	}
}

func TestRetryOnlyForGET(t *testing.T) {
	var gets, posts atomic.Int32
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			if gets.Add(1) < 3 {
				writeJSON(w, 503, ``)
				return
			}
			writeJSON(w, 200, `[]`)
			return
		}
		posts.Add(1)
		writeJSON(w, 503, ``)
	}, WithRetry(3, time.Millisecond))

	if _, err := c.GetBalances(context.Background()); err != nil {
		t.Fatalf("GET should succeed after retries: %v", err)
	}
	if gets.Load() != 3 {
		t.Errorf("GET attempts = %d, want 3", gets.Load())
	}
	if _, err := c.CancelTransaction(context.Background(), "t"); err == nil {
		t.Fatal("POST must surface the 503")
	}
	if posts.Load() != 1 {
		t.Errorf("POST attempts = %d, want 1", posts.Load())
	}
}

func TestRetryStopsOnContextCancel(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 503, ``) },
		WithRetry(10, time.Hour))
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := c.GetBalances(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected deadline exceeded, got %v", err)
	}
}

func TestWaitForFinalStatus(t *testing.T) {
	var n atomic.Int32
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		status := "PENDING"
		if n.Add(1) >= 3 {
			status = "CONFIRMED"
		}
		writeJSON(w, 200, `{"id":"t","status":"`+status+`"}`)
	})
	tx, err := c.WaitForFinalStatus(context.Background(), "t", time.Millisecond)
	if err != nil || tx.Status != StatusConfirmed {
		t.Fatalf("got %+v, %v", tx, err)
	}
}

func TestBaseURLWithPathPrefix(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.EscapedPath() != "/gateway/v1/transaction/a%2Fb" {
			t.Errorf("escaped path = %s", r.URL.EscapedPath())
		}
		writeJSON(w, 200, `{}`)
	}))
	defer srv.Close()
	c, err := New(testMerchant, testSecret, WithBaseURL(srv.URL+"/gateway/v1/"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.GetTransaction(context.Background(), "a/b"); err != nil {
		t.Fatal(err)
	}
}

func TestCancelEndpointsSendDocumentedAccept(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Accept"); got != "text/plain, application/json" {
			t.Errorf("%s Accept = %q", r.URL.Path, got)
		}
		writeJSON(w, 200, `{"supported":true}`)
	})
	if _, err := c.CheckCancel(context.Background(), "t"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.CancelTransaction(context.Background(), "t"); err != nil {
		t.Fatal(err)
	}
}

func TestLoggerNeverLeaksCredentials(t *testing.T) {
	var buf strings.Builder
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	var calls int
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if calls++; calls == 1 {
			writeJSON(w, 503, `{"message":"try again","secret":"`+testSecret+`"}`)
			return
		}
		writeJSON(w, 200, `[]`)
	}, WithLogger(logger), WithRetry(1, time.Millisecond), WithPayoutSecret("payout-secret-value"))

	if _, err := c.GetBalances(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := c.ListCards(context.Background(), true); err != nil {
		t.Fatal(err)
	}

	out := buf.String()
	for _, want := range []string{"platega request", "method=GET", "path=/balance/all", "status=503", "attempt=2", "will be retried", "path=/api/v1/cards"} {
		if !strings.Contains(out, want) {
			t.Errorf("log is missing %q:\n%s", want, out)
		}
	}
	for _, leak := range []string{testSecret, testMerchant, "payout-secret-value", "X-Secret", "Authorization", "PG-HMAC", "onlyActive", "try again"} {
		if strings.Contains(out, leak) {
			t.Errorf("log leaks %q:\n%s", leak, out)
		}
	}
}

func TestNoLoggerByDefault(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, `[]`) })
	if c.logger != nil {
		t.Error("logging must be off by default")
	}
	if _, err := c.GetBalances(context.Background()); err != nil {
		t.Fatal(err)
	}
}
