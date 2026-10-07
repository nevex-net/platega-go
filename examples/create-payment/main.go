// Command create-payment creates a payment link and waits for the result.
//
//	PLATEGA_MERCHANT_ID=... PLATEGA_SECRET=... go run ./examples/create-payment
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	platega "github.com/nevex-net/platega-go"
)

func main() {
	client, err := platega.New(os.Getenv("PLATEGA_MERCHANT_ID"), os.Getenv("PLATEGA_SECRET"))
	if err != nil {
		log.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()

	link, err := client.CreatePaymentLink(ctx, platega.CreatePaymentLinkRequest{
		PaymentParams: platega.PaymentParams{
			Details:     platega.PaymentDetails{Amount: platega.MustAmount("500"), Currency: "RUB"},
			Description: "Order #42",
			ReturnURL:   "https://example.com/success",
			FailedURL:   "https://example.com/fail",
			OrderID:     "42",
		},
	})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("pay here:", link.URL)

	// Polling keeps the example short; use callbacks in production.
	tx, err := client.WaitForFinalStatus(ctx, link.TransactionID, 5*time.Second)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("final status:", tx.Status)
}
