package platega

import (
	"context"
	"net/http"
)

// cancelAccept is what Platega documents (and its own SDKs send) for the
// cancellation endpoints.
const cancelAccept = "text/plain, application/json"

// CancelSupport tells whether a transaction can be cancelled (refunded).
type CancelSupport struct {
	// Supported is true when cancellation is available and a balance can cover it.
	Supported bool `json:"supported"`
	// TotalDeductUSDT is the amount that will be debited from the balance.
	TotalDeductUSDT Amount `json:"totalDeductUsdt"`
	// The penalty fields are unset (see [Amount.IsZero]) when there is no penalty.
	PenaltyNativeAmount   Amount  `json:"penaltyNativeAmount"`
	PenaltyNativeCurrency *string `json:"penaltyNativeCurrency"`
	PenaltyUSDT           Amount  `json:"penaltyUsdt"`
	PenaltyConversionRate Amount  `json:"penaltyConversionRate"`
	// BlockReason explains why Supported is false, e.g. "Insufficient funds".
	BlockReason *string `json:"blockReason"`
}

// CheckCancel checks whether a transaction can be cancelled and what it costs.
func (c *Client) CheckCancel(ctx context.Context, id string) (*CancelSupport, error) {
	seg, err := pathID(id)
	if err != nil {
		return nil, err
	}
	var out CancelSupport
	err = c.do(ctx, request{method: http.MethodGet, path: "/transaction/" + seg + "/cancel-supported", accept: cancelAccept}, &out)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// CancelResult is the outcome of a cancellation request.
type CancelResult struct {
	TransactionID string `json:"transactionId"`
	// Accepted is false when the cancellation needs manual handling.
	Accepted bool `json:"accepted"`
	// ManualControlRequired is true when the cancellation cannot be done
	// automatically and you must contact Platega support.
	ManualControlRequired bool   `json:"manualControlRequired"`
	Message               string `json:"message"`
}

// CancelTransaction starts a cancellation and a refund to the payer. Call
// [Client.CheckCancel] first.
func (c *Client) CancelTransaction(ctx context.Context, id string) (*CancelResult, error) {
	seg, err := pathID(id)
	if err != nil {
		return nil, err
	}
	var out CancelResult
	err = c.do(ctx, request{method: http.MethodPost, path: "/transaction/" + seg + "/cancel", accept: cancelAccept}, &out)
	if err != nil {
		return nil, err
	}
	return &out, nil
}
