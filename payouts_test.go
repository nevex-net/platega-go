package platega

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

// Expected signatures were produced by an independent Python implementation
// of the algorithm in the Platega documentation.
const (
	vectorSecret = "test-secret"
	vectorTS     = 1719403200
	vectorPOST   = "AyqNWO9GI9XeLzDk1xk9VesFvD5TNb4mCitBd01qdJs="
	vectorGET    = "JkLnWze7MAmjSuZoIOZdk5myWz0PRsoSDWvqo8XPplE="
	// The Payout API signs the path together with the query string.
	vectorGETQuery = "ALgqIAcLyu62CQxh4dpObrUBYgeMjcUqHiAAKKs7Lik="
	// Same GET /api/v1/cards, signed with "payout-secret".
	vectorGETOtherSecret = "FFsdCaHaRDlhfBSqF9PVSz0k5pY21Wszsh9bZLSWKIA="
)

const vectorBody = `{"cardNumber":"2200000000000000","amountRub":1500,"payoutMethod":"CARD","currencyRequested":"RUB"}`

func TestSignPGVectors(t *testing.T) {
	got := signPG(vectorSecret, "POST", "/api/v1/payouts/card-rub", vectorTS, "idem-1", []byte(vectorBody))
	if got != vectorPOST {
		t.Errorf("POST signature = %s, want %s", got, vectorPOST)
	}
	got = signPG(vectorSecret, "GET", "/api/v1/cards", vectorTS, "", nil)
	if got != vectorGET {
		t.Errorf("GET signature = %s, want %s", got, vectorGET)
	}
	got = signPG(vectorSecret, "GET", "/api/v1/cards?onlyActive=false", vectorTS, "", nil)
	if got != vectorGETQuery {
		t.Errorf("GET signature with query = %s, want %s", got, vectorGETQuery)
	}
}

func fixedClock() func() time.Time {
	return func() time.Time { return time.Unix(vectorTS, 0) }
}

func newPayoutClient(t *testing.T, h http.HandlerFunc) *Client {
	t.Helper()
	c := newTestClient(t, h)
	c.now = fixedClock()
	return c
}

func TestCreateCardPayout(t *testing.T) {
	c := newPayoutClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/payouts/card-rub" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		raw, _ := io.ReadAll(r.Body)
		if string(raw) != vectorBody {
			t.Errorf("body = %s", raw)
		}
		wantAuth := "PG-HMAC kid=" + testMerchant + ", ts=1719403200, sig=" + vectorPOST
		if got := r.Header.Get("Authorization"); got != wantAuth {
			t.Errorf("Authorization = %q, want %q", got, wantAuth)
		}
		if got := r.Header.Get("Idempotency-Key"); got != "idem-1" {
			t.Errorf("Idempotency-Key = %q", got)
		}
		if r.Header.Get("X-Secret") != "" {
			t.Error("HMAC auth must not leak X-Secret")
		}
		writeJSON(w, 200, `{"withdrawalRecordId":"w1","status":"CREATED","cardMasked":"**** 0000","amountUsdtDebited":13.27}`)
	})
	p, err := c.CreateCardPayout(context.Background(), CardPayoutRequest{
		IdempotencyKey: "idem-1", CardNumber: "2200000000000000", AmountRUB: 1500,
	})
	if err != nil {
		t.Fatal(err)
	}
	if p.WithdrawalRecordID != "w1" || p.Status != "CREATED" || p.AmountUSDTDebited.String() != "13.27" {
		t.Errorf("unexpected payout: %+v", p)
	}
}

func TestCreateCardPayoutValidation(t *testing.T) {
	c := newPayoutClient(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("request must not be sent")
	})
	cases := map[string]CardPayoutRequest{
		"no key":      {CardNumber: "1", AmountRUB: 1000},
		"no card":     {IdempotencyKey: "k", AmountRUB: 1000},
		"both cards":  {IdempotencyKey: "k", CardID: "a", CardNumber: "b", AmountRUB: 1000},
		"zero amount": {IdempotencyKey: "k", CardID: "a"},
	}
	for name, req := range cases {
		if _, err := c.CreateCardPayout(context.Background(), req); err == nil {
			t.Errorf("%s: expected validation error", name)
		}
	}
}

func TestListCards(t *testing.T) {
	var calls int
	c := newPayoutClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		wantSig := vectorGET
		if calls == 2 {
			wantSig = vectorGETQuery
		}
		wantAuth := "PG-HMAC kid=" + testMerchant + ", ts=1719403200, sig=" + wantSig
		if got := r.Header.Get("Authorization"); got != wantAuth {
			t.Errorf("call %d: Authorization = %q, want %q", calls, got, wantAuth)
		}
		if got := r.URL.Query().Get("onlyActive"); calls == 1 && got != "" || calls == 2 && got != "false" {
			t.Errorf("call %d: onlyActive = %q", calls, got)
		}
		writeJSON(w, 200, `[{"cardId":"c1","masked":"•••• 4242","last4":"4242","brand":"Visa","label":"main","status":"ACTIVE"}]`)
	})
	for _, includeInactive := range []bool{false, true} {
		cards, err := c.ListCards(context.Background(), includeInactive)
		if err != nil || len(cards) != 1 || cards[0].Last4 != "4242" {
			t.Fatalf("cards: %+v, %v", cards, err)
		}
	}
}

func TestPayoutSecretOverride(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		wantAuth := "PG-HMAC kid=" + testMerchant + ", ts=1719403200, sig=" + vectorGETOtherSecret
		if got := r.Header.Get("Authorization"); got != wantAuth {
			t.Errorf("Authorization = %q, want %q", got, wantAuth)
		}
		writeJSON(w, 200, `[]`)
	}, WithPayoutSecret("payout-secret"))
	c.now = fixedClock()
	if _, err := c.ListCards(context.Background(), false); err != nil {
		t.Fatal(err)
	}
}

func TestAPIErrorTruncatesBody(t *testing.T) {
	e := &APIError{StatusCode: 400, Method: "POST", Path: "/x", Body: []byte(strings.Repeat("я", 500))}
	if n := len([]rune(e.Error())); n > 260 {
		t.Errorf("error text too long: %d runes", n)
	}
}
