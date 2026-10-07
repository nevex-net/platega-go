// Package platega is an unofficial Go client for the Platega payment API
// (https://docs.platega.io).
//
// It has no dependencies outside the standard library.
//
// # Quick start
//
//	client, err := platega.New(merchantID, secret)
//	if err != nil {
//		return err
//	}
//	link, err := client.CreatePaymentLink(ctx, platega.CreatePaymentLinkRequest{
//		PaymentParams: platega.PaymentParams{
//			Details:     platega.PaymentDetails{Amount: platega.MustAmount("500"), Currency: "RUB"},
//			Description: "Order #42",
//			ReturnURL:   "https://example.com/success",
//			FailedURL:   "https://example.com/fail",
//			OrderID:     "42",
//		},
//	})
//	// send the payer to link.URL
//
// # Callbacks
//
// Platega reports status changes to a URL configured in the merchant
// dashboard. [CallbackHandler] authenticates and decodes them.
//
// # Amounts
//
// Money, crypto amounts and rates are [Amount] values: exact decimals that keep
// the digits Platega sent. Compare them with [Amount.Equal], not with ==, and
// do not convert them to float64 for anything that matters.
//
// # Errors and logging
//
// Non-2xx responses are [*APIError]; match categories with errors.Is and the
// Err* variables, or the IsNotFound-style helpers. [WithLogger] enables
// structured request logs that never contain credentials.
//
// # Payout API
//
// Saved cards and card payouts are signed with HMAC-SHA256 using the merchant
// secret; override it with [WithPayoutSecret] if your Payout API key differs.
//
// # Disclaimer
//
// This project is not affiliated with Platega. The API documentation is
// incomplete in places; see the README for the list of behaviours that are
// implemented from documentation only.
package platega
