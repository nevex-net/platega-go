package platega

import (
	"context"
	"errors"
	"net/http"
	"net/url"
)

// Payouts and saved cards belong to the Payout API. It has to be enabled by
// your Platega manager. Its requests are signed with HMAC-SHA256 using the
// merchant secret, or the secret from [WithPayoutSecret] if you set one.

// Card is a saved payout card.
type Card struct {
	CardID string `json:"cardId"`
	Masked string `json:"masked"`
	Last4  string `json:"last4"`
	Brand  string `json:"brand"`
	Label  string `json:"label"`
	// Status is ACTIVE, DISABLED or PENDING.
	Status string `json:"status"`
}

// ListCards returns saved payout cards. By default only ACTIVE cards are
// returned; set includeInactive to also get DISABLED and PENDING ones.
func (c *Client) ListCards(ctx context.Context, includeInactive bool) ([]Card, error) {
	var q url.Values
	if includeInactive {
		q = url.Values{"onlyActive": {"false"}}
	}
	var out []Card
	err := c.do(ctx, request{method: http.MethodGet, path: "/api/v1/cards", query: q, auth: authHMAC}, &out)
	if err != nil {
		return nil, err
	}
	return out, nil
}

// CardPayoutRequest creates a payout to a ruble card. Set exactly one of
// CardID and CardNumber.
type CardPayoutRequest struct {
	// IdempotencyKey is required and must be unique per payout (for example a
	// UUID). Reusing the key of a payout that already exists must not create a
	// second one, so persist it before the first attempt and reuse it on retry.
	IdempotencyKey string `json:"-"`
	// CardID is the ID of a saved card.
	CardID string `json:"cardId,omitempty"`
	// CardNumber is the full 16-digit recipient card number.
	CardNumber string `json:"cardNumber,omitempty"`
	// AmountRUB is the payout amount in whole rubles.
	AmountRUB int `json:"amountRub"`
}

type cardPayoutWire struct {
	CardID            string `json:"cardId,omitempty"`
	CardNumber        string `json:"cardNumber,omitempty"`
	AmountRUB         int    `json:"amountRub"`
	PayoutMethod      string `json:"payoutMethod"`
	CurrencyRequested string `json:"currencyRequested"`
}

// CardPayout is a created payout.
type CardPayout struct {
	WithdrawalRecordID string `json:"withdrawalRecordId"`
	// Status is CREATED right after creation.
	Status     string `json:"status"`
	CardMasked string `json:"cardMasked"`
	// AmountUSDTDebited is the sum debited from the merchant's USDT balance.
	AmountUSDTDebited Amount `json:"amountUsdtDebited"`
}

// CreateCardPayout pays out rubles to a card. The API enforces its own amount
// limits (at the time of writing, 1000 to 87500 RUB per payout).
func (c *Client) CreateCardPayout(ctx context.Context, req CardPayoutRequest) (*CardPayout, error) {
	switch {
	case req.IdempotencyKey == "":
		return nil, errors.New("platega: idempotency key is required")
	case (req.CardID == "") == (req.CardNumber == ""):
		return nil, errors.New("platega: set exactly one of CardID and CardNumber")
	case req.AmountRUB <= 0:
		return nil, errors.New("platega: amount must be positive")
	}
	wire := cardPayoutWire{
		CardID:            req.CardID,
		CardNumber:        req.CardNumber,
		AmountRUB:         req.AmountRUB,
		PayoutMethod:      "CARD",
		CurrencyRequested: "RUB",
	}
	var out CardPayout
	err := c.do(ctx, request{
		method:         http.MethodPost,
		path:           "/api/v1/payouts/card-rub",
		body:           wire,
		auth:           authHMAC,
		idempotencyKey: req.IdempotencyKey,
	}, &out)
	if err != nil {
		return nil, err
	}
	return &out, nil
}
