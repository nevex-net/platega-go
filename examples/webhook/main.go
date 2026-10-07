// Command webhook runs a callback receiver.
//
//	PLATEGA_MERCHANT_ID=... PLATEGA_SECRET=... go run ./examples/webhook
//
// Platega only delivers callbacks to public HTTPS URLs with a valid
// certificate, so put this behind a TLS-terminating proxy.
package main

import (
	"context"
	"log"
	"net/http"
	"os"

	platega "github.com/nevex-net/platega-go"
)

func main() {
	merchantID, secret := os.Getenv("PLATEGA_MERCHANT_ID"), os.Getenv("PLATEGA_SECRET")
	client, err := platega.New(merchantID, secret)
	if err != nil {
		log.Fatal(err)
	}

	http.Handle("/platega/callback", platega.CallbackHandler(merchantID, secret,
		func(ctx context.Context, cb *platega.Callback) error {
			switch cb.Kind() {
			case platega.CallbackSubscriptionStatus:
				log.Printf("subscription %s: %s", cb.SubscriptionID, cb.Status)
			case platega.CallbackSubscriptionCharge:
				log.Printf("subscription %s charge %s: %s", cb.SubscriptionID, cb.ID, cb.Status)
			default:
				// Look up the order you billed (here: from the payload you set
				// when creating the payment) and compare it with the callback.
				err := cb.CheckAgainst(platega.Expected{
					Amount:   platega.MustAmount("500"), // from your order
					Currency: "RUB",
				})
				if err != nil {
					log.Printf("ignoring suspicious callback %s: %v", cb.ID, err)
					return nil // acknowledge it, but do not fulfil the order
				}
				// Do not trust the callback alone: confirm with the API.
				tx, err := client.GetTransaction(ctx, cb.ID)
				if err != nil {
					return err // 500 makes Platega retry
				}
				if tx.Status == platega.StatusConfirmed {
					log.Printf("order paid: transaction %s, payload %q", tx.ID, tx.Payload)
				}
			}
			return nil
		}))

	log.Fatal(http.ListenAndServe(":8080", nil))
}
