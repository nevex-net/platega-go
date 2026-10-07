package platega_test

import (
	"context"
	"errors"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"

	platega "github.com/nevex-net/platega-go"
)

func ExampleNew() {
	client, err := platega.New("merchant-id", "secret",
		platega.WithRetry(2, 0), // retry idempotent GETs
	)
	if err != nil {
		log.Fatal(err)
	}
	_ = client
}

// Create a payment where the payer chooses the method on Platega's page, then
// send the payer to link.URL.
func ExampleClient_CreatePaymentLink() {
	client, _ := platega.New("merchant-id", "secret")

	link, err := client.CreatePaymentLink(context.Background(), platega.CreatePaymentLinkRequest{
		PaymentParams: platega.PaymentParams{
			Details:     platega.PaymentDetails{Amount: platega.MustAmount("500"), Currency: "RUB"},
			Description: "Order #42",
			ReturnURL:   "https://example.com/success",
			FailedURL:   "https://example.com/fail",
			OrderID:     "42",
			Metadata:    &platega.Metadata{UserID: "user-123"},
		},
	})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(link.URL, link.TransactionID)
}

// Fix the payment method up front.
func ExampleClient_CreatePayment() {
	client, _ := platega.New("merchant-id", "secret")

	payment, err := client.CreatePayment(context.Background(), platega.CreatePaymentRequest{
		PaymentMethod: platega.MethodSBPQR,
		PaymentParams: platega.PaymentParams{
			Details:     platega.PaymentDetails{Amount: platega.MustAmount("500"), Currency: "RUB"},
			Description: "Order #42",
			ReturnURL:   "https://example.com/success",
			FailedURL:   "https://example.com/fail",
		},
	})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(payment.Redirect)
}

func ExampleClient_GetTransaction() {
	client, _ := platega.New("merchant-id", "secret")

	tx, err := client.GetTransaction(context.Background(), "transaction-id")
	switch {
	case platega.IsNotFound(err):
		fmt.Println("unknown transaction")
	case err != nil:
		log.Fatal(err)
	case tx.Status == platega.StatusConfirmed:
		fmt.Println("paid")
	}
}

// Check that a refund is possible before asking for it.
func ExampleClient_CancelTransaction() {
	client, _ := platega.New("merchant-id", "secret")
	ctx := context.Background()

	support, err := client.CheckCancel(ctx, "transaction-id")
	if err != nil || !support.Supported {
		return
	}
	res, err := client.CancelTransaction(ctx, "transaction-id")
	if err != nil {
		log.Fatal(err)
	}
	if res.ManualControlRequired {
		fmt.Println("contact Platega support")
	}
}

func ExampleClient_CreateSubscription() {
	client, _ := platega.New("merchant-id", "secret")

	link, err := client.CreateSubscription(context.Background(), platega.CreateSubscriptionRequest{
		Amount:        500,
		Currency:      "RUB",
		Interval:      platega.IntervalMonth,
		IntervalCount: 1,
		Description:   "Premium",
	})
	if err != nil {
		log.Fatal(err)
	}
	// Send the payer to link.Redirect within 30 minutes and store
	// link.SubscriptionID: callbacks and later calls refer to it.
	fmt.Println(link.Redirect, link.SubscriptionID)
}

// Payout API methods need the separate payout secret.
func ExampleClient_CreateCardPayout() {
	client, _ := platega.New("merchant-id", "secret", platega.WithPayoutSecret("payout-secret"))

	payout, err := client.CreateCardPayout(context.Background(), platega.CardPayoutRequest{
		IdempotencyKey: "0b0f2a1e-6f2c-4a53-9a58-1d6d4c9d1c11", // persist before the first attempt
		CardID:         "saved-card-id",
		AmountRUB:      1500,
	})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(payout.WithdrawalRecordID)
}

// CallbackHandler authenticates and decodes Platega callbacks. Returning an
// error answers 500, which makes Platega retry the callback.
func ExampleCallbackHandler() {
	handler := platega.CallbackHandler("merchant-id", "secret",
		func(_ context.Context, cb *platega.Callback) error {
			switch cb.Kind() {
			case platega.CallbackSubscriptionStatus:
				fmt.Println("subscription", cb.SubscriptionID, cb.Status)
			case platega.CallbackSubscriptionCharge:
				fmt.Println("charge", cb.ID, cb.PaymentStatus())
			default:
				fmt.Println("transaction", cb.ID, cb.PaymentStatus())
			}
			return nil
		})

	// In a real service: http.Handle("/platega/callback", handler).
	req := httptest.NewRequest(http.MethodPost, "/platega/callback",
		strings.NewReader(`{"id":"t1","amount":500,"currency":"RUB","status":"CONFIRMED","paymentMethod":2}`))
	req.Header.Set("X-MerchantId", "merchant-id")
	req.Header.Set("X-Secret", "secret")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	fmt.Println(rec.Code)

	// Output:
	// transaction t1 CONFIRMED
	// 200
}

func ExampleParseExpiresIn() {
	d, err := platega.ParseExpiresIn("00:15:00")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(d)

	// Output: 15m0s
}

func ExamplePaymentStatus_IsFinal() {
	fmt.Println(platega.StatusPending.IsFinal(), platega.StatusConfirmed.IsFinal())

	// Output: false true
}

// Amounts are exact decimals: 500 and 500.00 are the same number.
func ExampleAmount() {
	order := platega.MustAmount("500")
	paid, err := platega.ParseAmount("500.00")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(order.Equal(paid), paid)

	usdt := platega.MustAmount("0.01236094") // keeps every digit
	fmt.Println(usdt, usdt.Cmp(platega.MustAmount("0.01236095")))

	// Output:
	// true 500.00
	// 0.01236094 -1
}

// Compare a callback with the order you billed before fulfilling it.
func ExampleCallback_CheckAgainst() {
	cb := &platega.Callback{
		ID:            "t1",
		Amount:        platega.MustAmount("500.00"),
		Currency:      "RUB",
		PaymentMethod: int(platega.MethodSBPQR),
	}

	err := cb.CheckAgainst(platega.Expected{
		Amount:        platega.MustAmount("500"),
		Currency:      "RUB",
		PaymentMethod: platega.MethodSBPQR,
	})
	fmt.Println("same order:", err)

	err = cb.CheckAgainst(platega.Expected{Amount: platega.MustAmount("900")})
	fmt.Println("wrong amount:", errors.Is(err, platega.ErrAmountMismatch))

	// Output:
	// same order: <nil>
	// wrong amount: true
}

func ExampleAPIError() {
	client, _ := platega.New("merchant-id", "secret")

	_, err := client.GetTransaction(context.Background(), "transaction-id")

	var apiErr *platega.APIError
	switch {
	case errors.Is(err, platega.ErrNotFound):
		fmt.Println("unknown transaction")
	case errors.Is(err, platega.ErrRateLimited):
		fmt.Println("slow down")
	case errors.As(err, &apiErr):
		fmt.Println(apiErr.StatusCode, apiErr.Message, apiErr.Header.Get("X-Request-Id"))
	}
}

// Request logs never contain credentials, headers or bodies.
func ExampleWithLogger() {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))

	client, err := platega.New("merchant-id", "secret", platega.WithLogger(logger))
	if err != nil {
		log.Fatal(err)
	}
	_ = client
}
