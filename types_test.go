package platega

import (
	"encoding/json"
	"testing"
)

func TestPaymentDetailsDecoding(t *testing.T) {
	var p PaymentDetails
	for _, in := range []string{`{"amount":12.5,"currency":"EUR"}`, `"12.5 EUR"`, `"12,5 EUR"`} {
		p = PaymentDetails{}
		if err := json.Unmarshal([]byte(in), &p); err != nil || p.Amount.String() != "12.5" || p.Currency != "EUR" {
			t.Errorf("%s -> %+v, %v", in, p, err)
		}
	}
	if err := json.Unmarshal([]byte(`"garbage"`), &p); err == nil {
		t.Error("garbage string must fail")
	}
}
