package platega

import (
	"context"
	"net/http"
	"testing"
	"time"
)

func TestCreateSubscription(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/transaction/process" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		body := readBody(t, r)
		if body["paymentMethod"] != float64(6) {
			t.Errorf("paymentMethod = %v, want 6", body["paymentMethod"])
		}
		d := body["paymentDetails"].(map[string]any)
		if d["amount"] != float64(500) || d["currency"] != "RUB" ||
			d["interval"] != float64(3) || d["intervalCount"] != float64(1) {
			t.Errorf("paymentDetails = %v", d)
		}
		writeJSON(w, 200, `{"paymentMethod":"Subscription","transactionId":"sub-1",
			"redirect":"https://pay.example/subscription/sub-1","status":"PENDING","merchantId":"m"}`)
	})
	l, err := c.CreateSubscription(context.Background(), CreateSubscriptionRequest{
		Amount: 500, Currency: "RUB", Interval: IntervalMonth, IntervalCount: 1, Description: "Premium",
	})
	if err != nil {
		t.Fatal(err)
	}
	if l.SubscriptionID != "sub-1" || l.Redirect == "" || l.Status != "PENDING" {
		t.Errorf("unexpected link: %+v", l)
	}
}

func TestCreateSubscriptionValidation(t *testing.T) {
	c, _ := New(testMerchant, testSecret, WithBaseURL("http://127.0.0.1:1"))
	good := CreateSubscriptionRequest{Amount: 1, Currency: "RUB", Interval: IntervalDay, IntervalCount: 1}
	bad := map[string]func(*CreateSubscriptionRequest){
		"amount":   func(r *CreateSubscriptionRequest) { r.Amount = 0 },
		"currency": func(r *CreateSubscriptionRequest) { r.Currency = "" },
		"interval": func(r *CreateSubscriptionRequest) { r.Interval = 9 },
		"count":    func(r *CreateSubscriptionRequest) { r.IntervalCount = 0 },
	}
	for name, mutate := range bad {
		r := good
		mutate(&r)
		if _, err := c.CreateSubscription(context.Background(), r); err == nil {
			t.Errorf("%s: expected validation error", name)
		}
	}
}

func TestGetSubscription(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/subscription/sub-1" {
			t.Errorf("path = %s", r.URL.Path)
		}
		writeJSON(w, 200, `{"id":"sub-1","status":"Active","amount":100,"currencyCode":"RUB",
			"intervalUnit":"Month","intervalCount":1,"startAt":"2026-07-08T09:00:00Z",
			"nextChargeAt":"2026-08-09T09:10:00Z","lastChargeAt":"","description":"Premium",
			"createdAt":"2026-07-08T09:00:00Z","customerEmail":"payer@example.com",
			"chargeMetrics":{"chargesTotal":1,"chargesSuccess":1,"chargesFailed":0,"totalAmount":100,
			"lastChargeAt":null,"nextChargeAt":"2026-08-09T09:10:00Z"}}`)
	})
	s, err := c.GetSubscription(context.Background(), "sub-1")
	if err != nil {
		t.Fatal(err)
	}
	if s.Status != SubscriptionActive || s.IntervalUnit != "Month" || s.Amount != 100 ||
		s.NextChargeAt.Month() != time.August || !s.LastChargeAt.IsZero() ||
		s.ChargeMetrics.ChargesSuccess != 1 {
		t.Errorf("unexpected subscription: %+v", s)
	}
}

func TestListSubscriptions(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("status") != "4" || q.Get("page") != "1" || q.Get("size") != "20" ||
			q.Get("from") != "2026-07-01T00:00:00.000Z" || q.Get("to") != "2026-07-31T23:59:59.000Z" {
			t.Errorf("query = %v", q)
		}
		writeJSON(w, 200, `{"items":[{"id":"a","status":4,"amount":100,"currencyCode":"RUB","intervalUnit":3,
			"intervalCount":1,"nextChargeAt":null,"lastChargeAt":null,"customerEmail":null,
			"description":"x","chargesCount":0,"createdAt":"2026-07-14T13:23:16.164247Z"}],
			"total":1,"page":1,"size":20}`)
	})
	list, err := c.ListSubscriptions(context.Background(), ListSubscriptionsParams{
		Status: 4, Page: 1, Size: 20,
		From: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
		To:   time.Date(2026, 7, 31, 23, 59, 59, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	if list.Total != 1 || len(list.Items) != 1 || list.Items[0].IntervalUnit != IntervalMonth ||
		list.Items[0].CustomerEmail != "" || !list.Items[0].NextChargeAt.IsZero() {
		t.Errorf("unexpected list: %+v", list)
	}
}

func TestListSubscriptionsOmitsEmptyFilters(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.RawQuery != "" {
			t.Errorf("unexpected query %q", r.URL.RawQuery)
		}
		writeJSON(w, 200, `{"items":[],"total":0,"page":1,"size":20}`)
	})
	if _, err := c.ListSubscriptions(context.Background(), ListSubscriptionsParams{}); err != nil {
		t.Fatal(err)
	}
}

func TestCancelSubscription(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/subscription/sub-1/cancel" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		writeJSON(w, 200, `{"subscriptionId":"sub-1","status":"cancelled"}`)
	})
	res, err := c.CancelSubscription(context.Background(), "sub-1")
	if err != nil || res.SubscriptionID != "sub-1" || res.Status != "cancelled" {
		t.Fatalf("got %+v, %v", res, err)
	}
}
