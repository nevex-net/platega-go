package platega

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func callbackRequest(method, body, merchant, secret string) *http.Request {
	r := httptest.NewRequest(method, "/callback", strings.NewReader(body))
	if merchant != "" {
		r.Header.Set("X-MerchantId", merchant)
	}
	if secret != "" {
		r.Header.Set("X-Secret", secret)
	}
	return r
}

func TestParseCallbackKinds(t *testing.T) {
	cases := []struct {
		name string
		body string
		kind CallbackKind
		id   string
	}{
		{
			"transaction",
			`{"id":"t1","amount":1000,"currency":"RUB","status":"CONFIRMED","paymentMethod":2,"payload":"p"}`,
			CallbackTransaction, "t1",
		},
		{
			"subscription charge",
			`{"Id":"ch1","Amount":100,"Currency":"RUB","Status":"CONFIRMED","PaymentMethod":6,"Payload":"",
			  "SubscriptionId":"sub1","NextChargeAt":"2026-08-09T09:10:00Z"}`,
			CallbackSubscriptionCharge, "ch1",
		},
		{
			"failed subscription charge",
			`{"Id":"ch2","Amount":100,"Currency":"RUB","Status":"CANCELED","PaymentMethod":6,"Payload":"",
			  "SubscriptionId":"sub1","NextChargeAt":null}`,
			CallbackSubscriptionCharge, "ch2",
		},
		{
			"subscription status",
			`{"Id":"sub1","Amount":100,"Currency":"RUB","Status":"SUBSCRIPTION_ACTIVATED","PaymentMethod":6,
			  "Payload":"","SubscriptionId":"sub1","NextChargeAt":"2026-08-09T09:10:00Z"}`,
			CallbackSubscriptionStatus, "sub1",
		},
	}
	for _, tc := range cases {
		cb, err := ParseCallback(callbackRequest(http.MethodPost, tc.body, testMerchant, testSecret), testMerchant, testSecret)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if cb.Kind() != tc.kind || cb.ID != tc.id {
			t.Errorf("%s: kind=%v id=%q", tc.name, cb.Kind(), cb.ID)
		}
	}
}

func TestParseCallbackFields(t *testing.T) {
	body := `{"Id":"ch1","Amount":100,"Currency":"RUB","Status":"CONFIRMED","PaymentMethod":6,"Payload":"x",
		"SubscriptionId":"sub1","NextChargeAt":"2026-08-09T09:10:00Z"}`
	cb, err := ParseCallback(callbackRequest(http.MethodPost, body, testMerchant, testSecret), testMerchant, testSecret)
	if err != nil {
		t.Fatal(err)
	}
	if cb.Amount.String() != "100" || cb.Currency != "RUB" || cb.PaymentStatus() != StatusConfirmed ||
		cb.PaymentMethod != 6 || cb.Payload != "x" || cb.SubscriptionID != "sub1" || cb.NextChargeAt.Day() != 9 {
		t.Errorf("unexpected callback: %+v", cb)
	}
}

func TestVerifyCallback(t *testing.T) {
	cases := []struct {
		name             string
		merchant, secret string
		want             bool
	}{
		{"ok", testMerchant, testSecret, true},
		{"wrong secret", testMerchant, "nope", false},
		{"wrong merchant", "other", testSecret, false},
		{"missing", "", "", false},
	}
	for _, tc := range cases {
		r := callbackRequest(http.MethodPost, `{}`, tc.merchant, tc.secret)
		if got := VerifyCallback(r, testMerchant, testSecret); got != tc.want {
			t.Errorf("%s: got %v", tc.name, got)
		}
	}
	// Empty configured credentials must never authenticate an empty request.
	if VerifyCallback(callbackRequest(http.MethodPost, `{}`, "", ""), "", "") {
		t.Error("empty credentials must not verify")
	}
}

func TestCallbackHandler(t *testing.T) {
	var got *Callback
	handlerErr := error(nil)
	h := CallbackHandler(testMerchant, testSecret, func(_ context.Context, cb *Callback) error {
		got = cb
		return handlerErr
	})
	body := `{"id":"t1","amount":10,"currency":"RUB","status":"CANCELED","paymentMethod":2}`

	do := func(r *http.Request) int {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w.Code
	}

	if code := do(callbackRequest(http.MethodGet, "", testMerchant, testSecret)); code != http.StatusMethodNotAllowed {
		t.Errorf("GET = %d", code)
	}
	if code := do(callbackRequest(http.MethodPost, body, testMerchant, "bad")); code != http.StatusUnauthorized {
		t.Errorf("bad auth = %d", code)
	}
	if got != nil {
		t.Fatal("handler must not run for unauthenticated requests")
	}
	if code := do(callbackRequest(http.MethodPost, `{not json`, testMerchant, testSecret)); code != http.StatusBadRequest {
		t.Errorf("bad json = %d", code)
	}
	if code := do(callbackRequest(http.MethodPost, body, testMerchant, testSecret)); code != http.StatusOK {
		t.Errorf("ok = %d", code)
	}
	if got == nil || got.ID != "t1" || got.PaymentStatus() != StatusCanceled {
		t.Errorf("handler got %+v", got)
	}
	handlerErr = errors.New("db down")
	if code := do(callbackRequest(http.MethodPost, body, testMerchant, testSecret)); code != http.StatusInternalServerError {
		t.Errorf("handler error = %d, want 500 so Platega retries", code)
	}
}

func TestCheckAgainst(t *testing.T) {
	cb := &Callback{ID: "t", Amount: MustAmount("500.00"), Currency: "rub", PaymentMethod: 2}
	ok := Expected{Amount: MustAmount("500"), Currency: "RUB", PaymentMethod: MethodSBPQR}
	if err := cb.CheckAgainst(ok); err != nil {
		t.Fatalf("matching callback rejected: %v", err)
	}

	cases := []struct {
		name string
		exp  Expected
		want error
	}{
		{"amount", Expected{Amount: MustAmount("500.01"), Currency: "RUB"}, ErrAmountMismatch},
		{"currency", Expected{Amount: MustAmount("500"), Currency: "USD"}, ErrCurrencyMismatch},
		{"method", Expected{Amount: MustAmount("500"), PaymentMethod: MethodCard}, ErrMethodMismatch},
	}
	for _, tc := range cases {
		if err := cb.CheckAgainst(tc.exp); !errors.Is(err, tc.want) {
			t.Errorf("%s: got %v, want %v", tc.name, err, tc.want)
		}
	}

	// Skipped checks, and a callback without a method passes the method check.
	if err := cb.CheckAgainst(Expected{Amount: MustAmount("500")}); err != nil {
		t.Errorf("currency and method are optional: %v", err)
	}
	noMethod := &Callback{Amount: MustAmount("1"), Currency: "RUB"}
	if err := noMethod.CheckAgainst(Expected{Amount: MustAmount("1"), PaymentMethod: MethodCard}); err != nil {
		t.Errorf("missing method must pass: %v", err)
	}
	// An unset callback amount must not match a positive order.
	if err := (&Callback{}).CheckAgainst(ok); !errors.Is(err, ErrAmountMismatch) {
		t.Errorf("unset amount: %v", err)
	}
}
