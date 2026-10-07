package platega

import (
	"encoding/json"
	"testing"
	"time"
)

func TestParseExpiresIn(t *testing.T) {
	cases := map[string]time.Duration{
		"00:15:00":   15 * time.Minute,
		"01:00:30":   time.Hour + 30*time.Second,
		"1.00:00:00": 24 * time.Hour,
	}
	for in, want := range cases {
		got, err := ParseExpiresIn(in)
		if err != nil || got != want {
			t.Errorf("ParseExpiresIn(%q) = %v, %v; want %v", in, got, err, want)
		}
	}
	for _, bad := range []string{"", "15", "aa:bb:cc", "00:15"} {
		if _, err := ParseExpiresIn(bad); err == nil {
			t.Errorf("ParseExpiresIn(%q) should fail", bad)
		}
	}
}

func TestTimeDecoding(t *testing.T) {
	var v struct {
		A, B, C, D Time
	}
	in := `{"A":null,"B":"","C":"2026-07-14T13:23:16.164247Z","D":"2026-06-15 13:44:13"}`
	if err := json.Unmarshal([]byte(in), &v); err != nil {
		t.Fatal(err)
	}
	if !v.A.IsZero() || !v.B.IsZero() {
		t.Error("null and empty must decode to the zero time")
	}
	if v.C.Year() != 2026 || v.C.Nanosecond() == 0 || v.D.Hour() != 13 {
		t.Errorf("unexpected times: %v %v", v.C, v.D)
	}
	if err := json.Unmarshal([]byte(`{"A":"yesterday"}`), &v); err == nil {
		t.Error("garbage time must fail")
	}
}
