package platega

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

const maxCallbackBytes = 1 << 20

// ErrInvalidCallbackAuth means the X-MerchantId / X-Secret headers of an
// incoming callback do not match the merchant credentials.
var ErrInvalidCallbackAuth = errors.New("platega: invalid callback credentials")

// Subscription status events delivered in [Callback.Status].
const (
	SubscriptionEventActivated = "SUBSCRIPTION_ACTIVATED"
	SubscriptionEventPastDue   = "SUBSCRIPTION_PAST_DUE"
	SubscriptionEventCancelled = "SUBSCRIPTION_CANCELLED"
	SubscriptionEventFailed    = "SUBSCRIPTION_FAILED"
)

// Callback is the body of every notification Platega sends to your callback
// URL. There are three kinds, told apart by [Callback.Kind]:
//
//   - a transaction status change: Status is CONFIRMED, CANCELED or CHARGEBACKED;
//   - a subscription charge: like a transaction callback, plus SubscriptionID
//     and NextChargeAt, and ID is the ID of the new charge transaction;
//   - a subscription status change: Status is one of the SubscriptionEvent*
//     values, and ID equals SubscriptionID.
//
// Field names are matched case-insensitively, so the PascalCase bodies of
// subscription callbacks decode into the same struct.
type Callback struct {
	ID            string `json:"id"`
	Amount        Amount `json:"amount"`
	Currency      string `json:"currency"`
	Status        string `json:"status"`
	PaymentMethod int    `json:"paymentMethod"`
	Payload       string `json:"payload"`

	SubscriptionID string `json:"subscriptionId"`
	NextChargeAt   Time   `json:"nextChargeAt"`
}

// CallbackKind classifies a [Callback].
type CallbackKind int

const (
	CallbackTransaction CallbackKind = iota
	CallbackSubscriptionCharge
	CallbackSubscriptionStatus
)

// Kind reports what kind of notification this is.
func (cb *Callback) Kind() CallbackKind {
	switch {
	case strings.HasPrefix(cb.Status, "SUBSCRIPTION_"):
		return CallbackSubscriptionStatus
	case cb.SubscriptionID != "":
		return CallbackSubscriptionCharge
	default:
		return CallbackTransaction
	}
}

// PaymentStatus returns Status as a [PaymentStatus]. It is meaningful for
// transaction and subscription-charge callbacks.
func (cb *Callback) PaymentStatus() PaymentStatus { return PaymentStatus(cb.Status) }

// Mismatches reported by [Callback.CheckAgainst].
var (
	ErrAmountMismatch   = errors.New("platega: callback amount does not match the order")
	ErrCurrencyMismatch = errors.New("platega: callback currency does not match the order")
	ErrMethodMismatch   = errors.New("platega: callback payment method does not match the order")
)

// Expected describes what you expect a callback to say about an order.
type Expected struct {
	// Amount is the order sum. It is required.
	Amount Amount
	// Currency is the order currency, compared case-insensitively. Empty skips
	// the check.
	Currency string
	// PaymentMethod is the method you asked for. Zero skips the check.
	PaymentMethod PaymentMethod
}

// CheckAgainst compares the callback with the order you created and returns
// [ErrAmountMismatch], [ErrCurrencyMismatch] or [ErrMethodMismatch] (wrapped,
// so use errors.Is) on the first difference. Amounts are compared numerically,
// so 500 matches 500.00.
//
// Call it before fulfilling an order: it guards against a callback for a
// different amount or currency than you billed. A callback that omits the
// payment method passes the method check.
func (cb *Callback) CheckAgainst(exp Expected) error {
	if !cb.Amount.Equal(exp.Amount) {
		return fmt.Errorf("%w: expected %s, got %s", ErrAmountMismatch, exp.Amount, cb.Amount)
	}
	if exp.Currency != "" && !strings.EqualFold(cb.Currency, exp.Currency) {
		return fmt.Errorf("%w: expected %s, got %s", ErrCurrencyMismatch, exp.Currency, cb.Currency)
	}
	if exp.PaymentMethod != 0 && cb.PaymentMethod != 0 && PaymentMethod(cb.PaymentMethod) != exp.PaymentMethod {
		return fmt.Errorf("%w: expected %d, got %d", ErrMethodMismatch, exp.PaymentMethod, cb.PaymentMethod)
	}
	return nil
}

// VerifyCallback reports whether the request carries the expected merchant
// credentials. The comparison is constant-time.
func VerifyCallback(r *http.Request, merchantID, secret string) bool {
	idOK := subtle.ConstantTimeCompare([]byte(r.Header.Get(headerMerchantID)), []byte(merchantID))
	secretOK := subtle.ConstantTimeCompare([]byte(r.Header.Get(headerSecret)), []byte(secret))
	return merchantID != "" && secret != "" && idOK&secretOK == 1
}

// ParseCallback verifies the credentials of an incoming callback request and
// decodes its body. It returns [ErrInvalidCallbackAuth] on a credentials
// mismatch.
func ParseCallback(r *http.Request, merchantID, secret string) (*Callback, error) {
	if !VerifyCallback(r, merchantID, secret) {
		return nil, ErrInvalidCallbackAuth
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, maxCallbackBytes))
	if err != nil {
		return nil, err
	}
	var cb Callback
	if err := json.Unmarshal(body, &cb); err != nil {
		return nil, err
	}
	return &cb, nil
}

// CallbackHandler returns an [http.Handler] that authenticates and decodes
// Platega callbacks and passes them to fn.
//
// It answers 405 for non-POST requests, 401 for wrong credentials, 400 for an
// unparseable body, 500 when fn returns an error and 200 otherwise. Platega
// retries a callback up to 3 more times, 5 minutes apart, unless it gets a
// successful response within 60 seconds, so fn must be idempotent and fast.
//
// Always confirm the final state with [Client.GetTransaction] before
// fulfilling an order if a forged callback would be costly.
func CallbackHandler(merchantID, secret string, fn func(ctx context.Context, cb *Callback) error) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		cb, err := ParseCallback(r, merchantID, secret)
		switch {
		case errors.Is(err, ErrInvalidCallbackAuth):
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		case err != nil:
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		if err := fn(r.Context(), cb); err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	})
}
