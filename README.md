# platega-go

[![ci](https://github.com/nevex-net/platega-go/actions/workflows/ci.yml/badge.svg)](https://github.com/nevex-net/platega-go/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/nevex-net/platega-go.svg)](https://pkg.go.dev/github.com/nevex-net/platega-go)

An unofficial Go client for the [Platega](https://platega.io) payment API
([docs](https://docs.platega.io)).

- Standard library only, no third-party dependencies.
- Exact decimal [`Amount`](#amounts) instead of `float64` for money, crypto
  amounts and rates.
- `context.Context` on every call, typed errors, injectable `http.Client`,
  optional `slog` logging that never contains credentials.
- Payments, status checks, refunds, balances, recurring SBP subscriptions,
  card payouts, and an `http.Handler` for callbacks.

> Not affiliated with Platega. Platega publishes no OpenAPI file and no Go SDK;
> this client was written from the public documentation. See
> [Known gaps](#known-gaps) before relying on a behaviour in production.

## Install

```sh
go get github.com/nevex-net/platega-go
```

Requires Go 1.22 or newer.

## Usage

### Create a payment

```go
client, err := platega.New(merchantID, secret)
if err != nil {
    return err
}

// The payer chooses the payment method on Platega's page:
link, err := client.CreatePaymentLink(ctx, platega.CreatePaymentLinkRequest{
    PaymentParams: platega.PaymentParams{
        Details:     platega.PaymentDetails{Amount: platega.MustAmount("500"), Currency: "RUB"},
        Description: "Order #42",
        ReturnURL:   "https://example.com/success",
        FailedURL:   "https://example.com/fail",
        OrderID:     "42",
        Metadata:    &platega.Metadata{UserID: "user-123"},
    },
})
// redirect the payer to link.URL, keep link.TransactionID

// Or fix the method up front:
p, err := client.CreatePayment(ctx, platega.CreatePaymentRequest{
    PaymentMethod: platega.MethodSBPQR,
    PaymentParams: params,
})
// redirect the payer to p.Redirect
```

### Check status, refund

```go
tx, err := client.GetTransaction(ctx, id)
if platega.IsNotFound(err) { /* unknown transaction */ }

support, err := client.CheckCancel(ctx, id)
if err == nil && support.Supported {
    res, err := client.CancelTransaction(ctx, id)
    _ = res.ManualControlRequired // true: contact Platega support
}
```

### Receive callbacks

```go
http.Handle("/platega/callback", platega.CallbackHandler(merchantID, secret,
    func(ctx context.Context, cb *platega.Callback) error {
        // Verify the final state with the API before fulfilling the order.
        tx, err := client.GetTransaction(ctx, cb.ID)
        if err != nil {
            return err // answered with 500, Platega retries
        }
        if tx.Status == platega.StatusConfirmed {
            // mark the order paid (idempotently!)
        }
        return nil
    }))
```

Before fulfilling an order, also compare the callback with what you billed:

```go
err := cb.CheckAgainst(platega.Expected{
    Amount:   order.Amount,   // 500 matches 500.00
    Currency: order.Currency, // optional, case-insensitive
})
if errors.Is(err, platega.ErrAmountMismatch) { /* do not fulfil */ }
```

The handler checks the `X-MerchantId` / `X-Secret` headers in constant time and
answers 405 / 401 / 400 / 500 / 200 as appropriate. Platega retries a callback
up to three more times, five minutes apart, if it does not get a success
response within 60 seconds, so your function must be fast and idempotent.
`cb.Kind()` tells transaction, subscription-charge and subscription-status
callbacks apart. Platega only calls HTTPS URLs with a valid public certificate.

### Recurring SBP subscriptions

```go
link, err := client.CreateSubscription(ctx, platega.CreateSubscriptionRequest{
    Amount: 500, Currency: "RUB",
    Interval: platega.IntervalMonth, IntervalCount: 1,
    Description: "Premium",
})
// send the payer to link.Redirect within 30 minutes; store link.SubscriptionID

sub, err := client.GetSubscription(ctx, link.SubscriptionID)
list, err := client.ListSubscriptions(ctx, platega.ListSubscriptionsParams{Page: 1, Size: 20})
_, err = client.CancelSubscription(ctx, link.SubscriptionID)
```

### Payout API (cards and payouts)

Enabled per merchant by Platega. Requests are signed with HMAC-SHA256 using the
merchant secret, as Platega's own SDKs do. If your Payout API key is a different
one, pass it with `WithPayoutSecret`:

```go
client, _ := platega.New(merchantID, secret) // or ..., platega.WithPayoutSecret(payoutSecret)

cards, err := client.ListCards(ctx, false)
payout, err := client.CreateCardPayout(ctx, platega.CardPayoutRequest{
    IdempotencyKey: payoutUUID, // persist before the first attempt, reuse on retry
    CardID:         cards[0].CardID,
    AmountRUB:      1500,
})
```

### Options

| Option | Purpose |
| --- | --- |
| `WithHTTPClient` | custom `http.Client`; wrap its `Transport` for tracing (e.g. `otelhttp`), metrics or proxies |
| `WithBaseURL` | sandbox or test server |
| `WithRetry(n, wait)` | retry idempotent GETs on network errors and 429/502/503/504 with exponential backoff and jitter, honouring `Retry-After`; state-changing calls are never retried |
| `WithLogger(*slog.Logger)` | log every attempt (method, path, status, duration, attempt); credentials, headers, query strings and bodies are never logged |
| `WithPayoutSecret` | sign Payout API requests with a key other than the merchant secret |
| `WithUserAgent` | custom `User-Agent` |

## Amounts

Platega returns sums, USDT amounts and rates as JSON numbers, some with eight
decimals (`0.01236094`). Decoding those into `float64` loses exactness and makes
comparisons unreliable, so every money field is an `Amount`: an exact decimal
that keeps the digits as they were sent.

```go
order := platega.MustAmount("500")
paid, _ := platega.ParseAmount("500.00")
order.Equal(paid)             // true: compared numerically, not as text
paid.String()                 // "500.00": digits kept as received
tx.AmountUSDT.Rat()           // *big.Rat for exact arithmetic
tx.AmountUSDT.Float64()       // nearest float64, for display only
```

`Amount` encodes back to a JSON number. The zero value means "not set" (a
missing or null field); `Sign()` is the numeric zero test. Never compare
amounts with `==`.

## Errors

Non-2xx responses are `*APIError` carrying the status, the `message` of a JSON
error body, the raw body, the response headers (for a request ID to quote to
support) and `Retry-After`. Match categories with `errors.Is`:

```go
switch {
case errors.Is(err, platega.ErrNotFound):    // 404
case errors.Is(err, platega.ErrUnauthorized): // 401
case errors.Is(err, platega.ErrRateLimited):  // 429
case errors.Is(err, platega.ErrServer):       // 5xx
}
var apiErr *platega.APIError
if errors.As(err, &apiErr) { log.Println(apiErr.Message, apiErr.Header.Get("X-Request-Id")) }
```

## API coverage

| Area | Methods |
| --- | --- |
| Payments | `CreatePayment`, `CreatePaymentLink`, `GetTransaction`, `WaitForFinalStatus`, `GetH2HQR` |
| Refunds | `CheckCancel`, `CancelTransaction` |
| Balances | `GetBalances` |
| Subscriptions | `CreateSubscription`, `GetSubscription`, `ListSubscriptions`, `CancelSubscription` |
| Payouts | `ListCards`, `CreateCardPayout` |
| Callbacks | `CallbackHandler`, `ParseCallback`, `VerifyCallback` |

See [Known gaps](#known-gaps) for what is not covered yet.

## Known gaps

The documentation is thin in places, so some behaviour is implemented from the
text and from Platega's official PHP, Python and Node.js SDKs, and has **not**
been verified against the live API:

- **Payout API signing** (`ListCards`, `CreateCardPayout`) follows the documented
  algorithm and is checked against an independent implementation. Following the
  official SDKs, the signed path includes the query string and the key is the
  merchant secret. If your key differs, use `WithPayoutSecret`.
- **Error bodies** are undocumented. The `message` field is taken from the
  official SDKs; everything else is exposed raw.
- **Subscription list status codes** are undocumented, so `SubscriptionSummary.Status`
  is the raw number.
- **Field spellings:** `Transaction` uses the API's own misspellings
  (`mechantId`, `comission`, `comissionUsdt`).
- **Cancel endpoints** are called with `Accept: text/plain, application/json`,
  as documented and as the official SDKs do.
- **H2H QR:** the docs say `GET /h2h/{id}`, the official SDKs call
  `GET /transaction/{id}/qr`. Only the documented path is implemented.
- **Not implemented** because their responses are undocumented or contradict
  their description: transaction exports (CSV / Excel / JSON), payment-method
  rates (`/rates/payment_method_rate`) and balance unlock operations
  (`/transaction/balance-unlock-operations`) that the official SDKs expose.

Verified a gap? A pull request or an issue is welcome.

## Development

```sh
go vet ./...
go test -race ./...
golangci-lint run
```

Tests run against `httptest` servers; no network or credentials needed.
See [CONTRIBUTING.md](CONTRIBUTING.md) for the contribution workflow and
[CHANGELOG.md](CHANGELOG.md) for release notes. Runnable usage examples live in
[`example_test.go`](example_test.go) (rendered on pkg.go.dev) and
[`examples/`](examples).

## Security

Please report vulnerabilities privately, as described in
[SECURITY.md](SECURITY.md).

## License

MIT
