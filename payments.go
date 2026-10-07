package platega

import (
	"context"
	"errors"
	"net/http"
	"time"
)

// CreatePaymentRequest creates a payment with a fixed payment method.
type CreatePaymentRequest struct {
	PaymentMethod PaymentMethod `json:"paymentMethod"`
	PaymentParams
}

// Payment is the result of [Client.CreatePayment].
type Payment struct {
	// PaymentMethod is the human-readable method name, e.g. "SBPQR".
	PaymentMethod string `json:"paymentMethod"`
	TransactionID string `json:"transactionId"`
	// Redirect is the URL the payer must open to pay.
	Redirect       string         `json:"redirect"`
	Return         string         `json:"return"`
	PaymentDetails PaymentDetails `json:"paymentDetails"`
	Status         PaymentStatus  `json:"status"`
	// ExpiresIn is the time left to pay, formatted HH:MM:SS.
	// See [ParseExpiresIn].
	ExpiresIn  string `json:"expiresIn"`
	MerchantID string `json:"merchantId"`
	USDTRate   Amount `json:"usdtRate"`
}

// CreatePayment creates a transaction paid with the given method and returns
// the URL to send the payer to.
func (c *Client) CreatePayment(ctx context.Context, req CreatePaymentRequest) (*Payment, error) {
	if req.PaymentMethod == 0 {
		return nil, errors.New("platega: payment method is required")
	}
	if err := req.validate(); err != nil {
		return nil, err
	}
	var out Payment
	err := c.do(ctx, request{method: http.MethodPost, path: "/transaction/process", body: req}, &out)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// CreatePaymentLinkRequest creates a payment where the payer chooses the
// method on the payment page.
type CreatePaymentLinkRequest struct {
	PaymentParams
}

// PaymentLink is the result of [Client.CreatePaymentLink].
type PaymentLink struct {
	TransactionID string        `json:"transactionId"`
	Status        PaymentStatus `json:"status"`
	// URL is the payment page the payer must open.
	URL       string `json:"url"`
	ExpiresIn string `json:"expiresIn"`
	Rate      Amount `json:"rate"`
}

// CreatePaymentLink creates a transaction without a fixed method; the payer
// picks one on the payment page.
func (c *Client) CreatePaymentLink(ctx context.Context, req CreatePaymentLinkRequest) (*PaymentLink, error) {
	if err := req.validate(); err != nil {
		return nil, err
	}
	var out PaymentLink
	err := c.do(ctx, request{method: http.MethodPost, path: "/v2/transaction/process", body: req}, &out)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// Transaction is the state of a transaction.
type Transaction struct {
	ID             string         `json:"id"`
	Status         PaymentStatus  `json:"status"`
	PaymentDetails PaymentDetails `json:"paymentDetails"`
	MerchantName   string         `json:"merchantName"`
	// MerchantID is serialised by the API as "mechantId" (sic).
	MerchantID string `json:"mechantId"`
	// Commission is serialised by the API as "comission" (sic).
	Commission    Amount `json:"comission"`
	PaymentMethod string `json:"paymentMethod"`
	ExpiresIn     string `json:"expiresIn"`
	Return        string `json:"return"`
	// CommissionUSDT is serialised by the API as "comissionUsdt" (sic).
	CommissionUSDT    Amount `json:"comissionUsdt"`
	AmountUSDT        Amount `json:"amountUsdt"`
	QR                string `json:"qr"`
	PayformSuccessURL string `json:"payformSuccessUrl"`
	Payload           string `json:"payload"`
	CommissionType    int    `json:"comissionType"`
	ExternalID        string `json:"externalId"`
	Description       string `json:"description"`
}

// GetTransaction returns the status and details of a transaction.
// Use [IsNotFound] to detect an unknown ID.
func (c *Client) GetTransaction(ctx context.Context, id string) (*Transaction, error) {
	seg, err := pathID(id)
	if err != nil {
		return nil, err
	}
	var out Transaction
	if err := c.do(ctx, request{method: http.MethodGet, path: "/transaction/" + seg}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// WaitForFinalStatus polls [Client.GetTransaction] every interval until the
// transaction leaves PENDING or ctx is done. Prefer callbacks in production;
// this is meant for scripts and tests.
func (c *Client) WaitForFinalStatus(ctx context.Context, id string, interval time.Duration) (*Transaction, error) {
	if interval <= 0 {
		interval = 3 * time.Second
	}
	for {
		tx, err := c.GetTransaction(ctx, id)
		if err != nil {
			return nil, err
		}
		if tx.Status.IsFinal() {
			return tx, nil
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(interval):
		}
	}
}

// H2HQR is the payment data of an H2H transaction.
type H2HQR struct {
	Amount Amount `json:"amount"`
	// QR is a QR payload or a payment link.
	QR string `json:"qr"`
}

// GetH2HQR returns the QR code or payment link of an H2H transaction. H2H mode
// has to be enabled for your account by your Platega manager.
func (c *Client) GetH2HQR(ctx context.Context, id string) (*H2HQR, error) {
	seg, err := pathID(id)
	if err != nil {
		return nil, err
	}
	var out H2HQR
	if err := c.do(ctx, request{method: http.MethodGet, path: "/h2h/" + seg}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}
