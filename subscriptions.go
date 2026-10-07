package platega

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// SubscriptionInterval is the billing period unit of a recurring subscription.
type SubscriptionInterval int

const (
	IntervalDay   SubscriptionInterval = 1 // up to 31 periods
	IntervalWeek  SubscriptionInterval = 2 // up to 4 periods
	IntervalMonth SubscriptionInterval = 3 // up to 12 periods
	IntervalYear  SubscriptionInterval = 4 // up to 3 periods
)

// SubscriptionStatus is the status string returned by [Client.GetSubscription].
type SubscriptionStatus string

const (
	SubscriptionPendingAgreement SubscriptionStatus = "PendingAgreement"
	SubscriptionActive           SubscriptionStatus = "Active"
	SubscriptionPastDue          SubscriptionStatus = "PastDue"
	SubscriptionCancelled        SubscriptionStatus = "Cancelled"
	SubscriptionFailed           SubscriptionStatus = "Failed"
)

// CreateSubscriptionRequest creates a recurring SBP subscription.
type CreateSubscriptionRequest struct {
	// Amount is the sum of one regular charge.
	Amount   int
	Currency string
	Interval SubscriptionInterval
	// IntervalCount is the number of Interval units between charges.
	IntervalCount int
	// Description is shown to the payer on the payment page and in emails.
	Description string
}

// SubscriptionLink is the result of [Client.CreateSubscription].
type SubscriptionLink struct {
	PaymentMethod string `json:"paymentMethod"`
	// SubscriptionID is the ID of the subscription (the API names this field
	// "transactionId"). Store it: callbacks and all other calls use it.
	SubscriptionID string `json:"transactionId"`
	// Redirect is where the payer confirms the bank account binding. Send the
	// payer there immediately: after 30 minutes the subscription becomes Failed.
	Redirect   string `json:"redirect"`
	Status     string `json:"status"`
	MerchantID string `json:"merchantId"`
}

type createSubscriptionWire struct {
	PaymentMethod  PaymentMethod       `json:"paymentMethod"`
	PaymentDetails subscriptionDetails `json:"paymentDetails"`
	Description    string              `json:"description"`
}

type subscriptionDetails struct {
	Amount        int                  `json:"amount"`
	Currency      string               `json:"currency"`
	Interval      SubscriptionInterval `json:"interval"`
	IntervalCount int                  `json:"intervalCount"`
}

// CreateSubscription creates a subscription and returns the URL where the
// payer binds their bank account. No money moves until the payer confirms;
// charges are then made automatically and reported via callbacks.
func (c *Client) CreateSubscription(ctx context.Context, req CreateSubscriptionRequest) (*SubscriptionLink, error) {
	switch {
	case req.Amount <= 0:
		return nil, errors.New("platega: amount must be positive")
	case req.Currency == "":
		return nil, errors.New("platega: currency is required")
	case req.Interval < IntervalDay || req.Interval > IntervalYear:
		return nil, errors.New("platega: invalid subscription interval")
	case req.IntervalCount <= 0:
		return nil, errors.New("platega: interval count must be positive")
	}
	wire := createSubscriptionWire{
		PaymentMethod: MethodSubscription,
		PaymentDetails: subscriptionDetails{
			Amount:        req.Amount,
			Currency:      req.Currency,
			Interval:      req.Interval,
			IntervalCount: req.IntervalCount,
		},
		Description: req.Description,
	}
	var out SubscriptionLink
	err := c.do(ctx, request{method: http.MethodPost, path: "/transaction/process", body: wire}, &out)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// ChargeMetrics are charge counters of a subscription.
type ChargeMetrics struct {
	ChargesTotal   int  `json:"chargesTotal"`
	ChargesSuccess int  `json:"chargesSuccess"`
	ChargesFailed  int  `json:"chargesFailed"`
	TotalAmount    int  `json:"totalAmount"`
	LastChargeAt   Time `json:"lastChargeAt"`
	NextChargeAt   Time `json:"nextChargeAt"`
}

// Subscription is a subscription as returned by [Client.GetSubscription].
type Subscription struct {
	ID       string             `json:"id"`
	Status   SubscriptionStatus `json:"status"`
	Amount   int                `json:"amount"`
	Currency string             `json:"currencyCode"`
	// IntervalUnit is the unit name, e.g. "Month".
	IntervalUnit  string        `json:"intervalUnit"`
	IntervalCount int           `json:"intervalCount"`
	StartAt       Time          `json:"startAt"`
	NextChargeAt  Time          `json:"nextChargeAt"`
	LastChargeAt  Time          `json:"lastChargeAt"`
	Description   string        `json:"description"`
	CreatedAt     Time          `json:"createdAt"`
	CustomerEmail string        `json:"customerEmail"`
	ChargeMetrics ChargeMetrics `json:"chargeMetrics"`
}

// GetSubscription returns a subscription by ID.
func (c *Client) GetSubscription(ctx context.Context, id string) (*Subscription, error) {
	seg, err := pathID(id)
	if err != nil {
		return nil, err
	}
	var out Subscription
	if err := c.do(ctx, request{method: http.MethodGet, path: "/subscription/" + seg}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// SubscriptionSummary is one row of [Client.ListSubscriptions].
//
// Unlike [Subscription], the list endpoint returns numeric codes for the
// status and the interval unit, and Platega does not document the status
// codes, so Status is the raw number. IntervalUnit uses [SubscriptionInterval].
type SubscriptionSummary struct {
	ID            string               `json:"id"`
	Status        int                  `json:"status"`
	Amount        int                  `json:"amount"`
	Currency      string               `json:"currencyCode"`
	IntervalUnit  SubscriptionInterval `json:"intervalUnit"`
	IntervalCount int                  `json:"intervalCount"`
	NextChargeAt  Time                 `json:"nextChargeAt"`
	LastChargeAt  Time                 `json:"lastChargeAt"`
	CustomerEmail string               `json:"customerEmail"`
	Description   string               `json:"description"`
	ChargesCount  int                  `json:"chargesCount"`
	CreatedAt     Time                 `json:"createdAt"`
}

// SubscriptionList is a page of subscriptions.
type SubscriptionList struct {
	Items []SubscriptionSummary `json:"items"`
	Total int                   `json:"total"`
	Page  int                   `json:"page"`
	Size  int                   `json:"size"`
}

// ListSubscriptionsParams filters [Client.ListSubscriptions]. Zero values mean
// "not set".
type ListSubscriptionsParams struct {
	// Status is the raw numeric status code to filter by.
	Status   int
	From, To time.Time
	Page     int
	Size     int
}

// ListSubscriptions returns a page of the merchant's subscriptions.
func (c *Client) ListSubscriptions(ctx context.Context, p ListSubscriptionsParams) (*SubscriptionList, error) {
	q := url.Values{}
	if p.Status != 0 {
		q.Set("status", strconv.Itoa(p.Status))
	}
	const layout = "2006-01-02T15:04:05.000Z"
	if !p.From.IsZero() {
		q.Set("from", p.From.UTC().Format(layout))
	}
	if !p.To.IsZero() {
		q.Set("to", p.To.UTC().Format(layout))
	}
	if p.Page > 0 {
		q.Set("page", strconv.Itoa(p.Page))
	}
	if p.Size > 0 {
		q.Set("size", strconv.Itoa(p.Size))
	}
	var out SubscriptionList
	if err := c.do(ctx, request{method: http.MethodGet, path: "/subscription", query: q}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// CancelledSubscription is the result of [Client.CancelSubscription].
type CancelledSubscription struct {
	SubscriptionID string `json:"subscriptionId"`
	Status         string `json:"status"`
}

// CancelSubscription stops future charges. The call is idempotent.
func (c *Client) CancelSubscription(ctx context.Context, id string) (*CancelledSubscription, error) {
	seg, err := pathID(id)
	if err != nil {
		return nil, err
	}
	var out CancelledSubscription
	err = c.do(ctx, request{method: http.MethodPost, path: "/subscription/" + seg + "/cancel"}, &out)
	if err != nil {
		return nil, err
	}
	return &out, nil
}
