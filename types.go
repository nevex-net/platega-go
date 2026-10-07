package platega

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

// PaymentStatus is the status of a transaction.
type PaymentStatus string

const (
	StatusPending      PaymentStatus = "PENDING"
	StatusCanceled     PaymentStatus = "CANCELED"
	StatusConfirmed    PaymentStatus = "CONFIRMED"
	StatusChargebacked PaymentStatus = "CHARGEBACKED"
)

// IsFinal reports whether the status can no longer change to PENDING.
func (s PaymentStatus) IsFinal() bool { return s != StatusPending && s != "" }

// PaymentMethod identifies a payment method.
type PaymentMethod int

const (
	MethodSBPQR         PaymentMethod = 2  // SBP (QR code)
	MethodERIP          PaymentMethod = 3  // ERIP
	MethodCard          PaymentMethod = 11 // card acquiring
	MethodInternational PaymentMethod = 12 // international payments
	MethodCrypto        PaymentMethod = 13 // cryptocurrency
	MethodSberPay       PaymentMethod = 14 // SberPay

	// MethodSubscription is the method value used by recurring SBP
	// subscriptions. [Client.CreateSubscription] sets it for you.
	MethodSubscription PaymentMethod = 6
)

// PaymentDetails is an amount with a currency.
//
// When decoding, it accepts both the object form {"amount":100,"currency":"RUB"}
// and the string form "100 RUB" that some endpoints return.
type PaymentDetails struct {
	Amount   Amount `json:"amount"`
	Currency string `json:"currency"`
}

// UnmarshalJSON implements json.Unmarshaler.
func (p *PaymentDetails) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	if len(b) == 0 || bytes.Equal(b, []byte("null")) {
		return nil
	}
	if b[0] == '"' {
		var s string
		if err := json.Unmarshal(b, &s); err != nil {
			return err
		}
		fields := strings.Fields(s)
		if len(fields) != 2 {
			return fmt.Errorf("platega: unexpected payment details %q", s)
		}
		amount, err := ParseAmount(strings.ReplaceAll(fields[0], ",", "."))
		if err != nil {
			return fmt.Errorf("platega: unexpected payment details %q: %w", s, err)
		}
		p.Amount, p.Currency = amount, fields[1]
		return nil
	}
	type plain PaymentDetails
	return json.Unmarshal(b, (*plain)(p))
}

// Metadata describes the payer. Some merchant categories must send UserID;
// omitting it when required disables Platega's anti-fraud protection and can
// lead to the shop being disabled. Ask your Platega manager whether it
// applies to you.
type Metadata struct {
	UserID   string `json:"userId,omitempty"`
	UserName string `json:"userName,omitempty"`
	ClientIP string `json:"clientIp,omitempty"`
}

// PaymentParams are the fields shared by all payment-creation requests.
type PaymentParams struct {
	Details     PaymentDetails `json:"paymentDetails"`
	Description string         `json:"description"`
	// ReturnURL is where the payer is sent after a successful payment.
	ReturnURL string `json:"return"`
	// FailedURL is where the payer is sent after a failed payment.
	FailedURL string `json:"failedUrl"`
	// Payload is free-form data echoed back in callbacks.
	Payload string `json:"payload,omitempty"`
	// OrderID is your internal order identifier.
	OrderID  string    `json:"orderId,omitempty"`
	Metadata *Metadata `json:"metadata,omitempty"`
}

func (p PaymentParams) validate() error {
	if p.Details.Amount.Sign() <= 0 {
		return fmt.Errorf("platega: amount must be positive")
	}
	if p.Details.Currency == "" {
		return fmt.Errorf("platega: currency is required")
	}
	return nil
}
