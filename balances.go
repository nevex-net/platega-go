package platega

import (
	"context"
	"net/http"
)

// Balance is one merchant balance.
type Balance struct {
	Amount        Amount `json:"amount"`
	Currency      string `json:"currency"`
	FrozenBalance Amount `json:"frozenBalance"`
}

// GetBalances returns all merchant balances.
func (c *Client) GetBalances(ctx context.Context) ([]Balance, error) {
	var out []Balance
	if err := c.do(ctx, request{method: http.MethodGet, path: "/balance/all"}, &out); err != nil {
		return nil, err
	}
	return out, nil
}
