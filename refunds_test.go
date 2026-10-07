package platega

import (
	"context"
	"net/http"
	"testing"
)

func TestRefunds(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/transaction/t/cancel-supported":
			writeJSON(w, 200, `{"supported":true,"totalDeductUsdt":0.0123,"penaltyNativeAmount":null,
				"penaltyNativeCurrency":null,"penaltyUsdt":null,"penaltyConversionRate":null,"blockReason":null}`)
		case r.Method == http.MethodPost && r.URL.Path == "/transaction/t/cancel":
			writeJSON(w, 200, `{"transactionId":"t","accepted":false,"manualControlRequired":true,"message":"in progress"}`)
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
	})
	s, err := c.CheckCancel(context.Background(), "t")
	if err != nil || !s.Supported || s.TotalDeductUSDT.String() != "0.0123" || !s.PenaltyUSDT.IsZero() || s.BlockReason != nil {
		t.Fatalf("cancel support: %+v, %v", s, err)
	}
	res, err := c.CancelTransaction(context.Background(), "t")
	if err != nil || res.Accepted || !res.ManualControlRequired || res.Message != "in progress" {
		t.Fatalf("cancel result: %+v, %v", res, err)
	}
}
