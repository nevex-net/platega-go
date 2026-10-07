package platega

import (
	"encoding/json"
	"math/big"
	"strings"
	"testing"
)

func TestParseAmountNormalises(t *testing.T) {
	cases := map[string]string{
		"500":        "500",
		"500.00":     "500.00", // trailing zeros are kept as received
		"0.01236094": "0.01236094",
		"10.8988764": "10.8988764",
		"-1.5":       "-1.5",
		"+7":         "7",
		"007":        "7",
		".5":         "0.5",
		"1e2":        "100",
		"1E-7":       "0.0000001",
		"1.5e3":      "1500",
		"0.5e1":      "5",
		"12.345e-2":  "0.12345",
		"-0":         "0",
		"-0.00":      "0.00",
		" 42 ":       "42",
	}
	for in, want := range cases {
		a, err := ParseAmount(in)
		if err != nil {
			t.Errorf("ParseAmount(%q): %v", in, err)
			continue
		}
		if a.String() != want {
			t.Errorf("ParseAmount(%q) = %q, want %q", in, a, want)
		}
	}
}

func TestParseAmountRejects(t *testing.T) {
	for _, in := range []string{"", " ", "abc", "1.2.3", "1e", "e5", "--1", "1 000", "0x10", "NaN", "Inf",
		"1e999", "1e-999", "12345678901234567890123456789012345678901234567890123456789012345",
		"." + strings.Repeat("0", 70) + "1"} {
		if a, err := ParseAmount(in); err == nil {
			t.Errorf("ParseAmount(%q) = %q, want error", in, a)
		}
	}
}

func TestAmountNumericComparison(t *testing.T) {
	if !MustAmount("500").Equal(MustAmount("500.00")) || !MustAmount("5e2").Equal(MustAmount("500")) {
		t.Error("500, 500.00 and 5e2 must be equal")
	}
	if MustAmount("0.1").Equal(MustAmount("0.10000001")) {
		t.Error("different values must not be equal")
	}
	if MustAmount("0.01236094").Cmp(MustAmount("0.01236095")) != -1 || MustAmount("2").Cmp(MustAmount("1.99999999")) != 1 {
		t.Error("Cmp must be exact")
	}
	var unset Amount
	if !unset.IsZero() || unset.Sign() != 0 || !unset.Equal(MustAmount("0")) || unset.String() != "" {
		t.Errorf("unset amount: %+v", unset)
	}
	if MustAmount("0.00").IsZero() {
		t.Error("numeric zero is not the unset zero value")
	}
	if MustAmount("-3").Sign() != -1 || MustAmount("3").Sign() != 1 {
		t.Error("Sign")
	}
}

// The reason Amount exists: float64 cannot hold these exactly, big.Rat can.
func TestAmountIsExactWhereFloatIsNot(t *testing.T) {
	// Variables, not constants: constant arithmetic in Go is exact and would hide the problem.
	x, y := 0.1, 0.2
	if x+y == 0.3 {
		t.Fatal("test premise broken: float64 should not add 0.1 and 0.2 to exactly 0.3")
	}

	sum := new(big.Rat).Add(MustAmount("0.1").Rat(), MustAmount("0.2").Rat())
	if sum.Cmp(MustAmount("0.3").Rat()) != 0 {
		t.Error("0.1 + 0.2 must equal 0.3 exactly")
	}
	// 2^53 + 1 is the first integer float64 cannot represent.
	if got := MustAmount("9007199254740993").String(); got != "9007199254740993" {
		t.Errorf("large integer changed: %s", got)
	}
	if got := MustAmount("0.01236094").Float64(); got != 0.01236094 {
		t.Errorf("Float64 = %v", got)
	}
}

func TestAmountJSON(t *testing.T) {
	type doc struct {
		A Amount  `json:"a"`
		B Amount  `json:"b"`
		C Amount  `json:"c"`
		D *Amount `json:"d"`
	}
	var d doc
	in := `{"a":10.8988764,"b":"1.64044944","c":null,"d":1e-7}`
	if err := json.Unmarshal([]byte(in), &d); err != nil {
		t.Fatal(err)
	}
	if d.A.String() != "10.8988764" || d.B.String() != "1.64044944" || !d.C.IsZero() || d.D == nil || d.D.String() != "0.0000001" {
		t.Errorf("decoded %+v", d)
	}

	out, err := json.Marshal(doc{A: MustAmount("500"), B: MustAmount("0.01236094")})
	if err != nil {
		t.Fatal(err)
	}
	// Numbers on the wire, never strings; unset becomes null.
	if want := `{"a":500,"b":0.01236094,"c":null,"d":null}`; string(out) != want {
		t.Errorf("encoded %s, want %s", out, want)
	}

	for _, bad := range []string{`{"a":"abc"}`, `{"a":true}`, `{"a":[1]}`, `{"a":{}}`} {
		if err := json.Unmarshal([]byte(bad), &d); err == nil {
			t.Errorf("%s must fail to decode", bad)
		}
	}
}

func TestAmountFromInt(t *testing.T) {
	if got := AmountFromInt(-42).String(); got != "-42" {
		t.Errorf("got %s", got)
	}
}

func TestMustAmountPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("MustAmount must panic on bad input")
		}
	}()
	MustAmount("nope")
}

func FuzzParseAmount(f *testing.F) {
	for _, s := range []string{"500", "0.01236094", "-1.5", "1e-7", "", "e", "1.2.3", ".5e+3"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		a, err := ParseAmount(s)
		if err != nil {
			return
		}
		// Whatever parses must round-trip through its own string and JSON.
		b, err := ParseAmount(a.String())
		if err != nil || !a.Equal(b) || a.String() != b.String() {
			t.Fatalf("%q -> %q -> %q (%v)", s, a, b, err)
		}
		raw, err := json.Marshal(a)
		if err != nil || !json.Valid(raw) {
			t.Fatalf("%q marshals to invalid JSON %s (%v)", a, raw, err)
		}
		var back Amount
		if err := json.Unmarshal(raw, &back); err != nil || !back.Equal(a) {
			t.Fatalf("JSON round trip of %q: %q (%v)", a, back, err)
		}
	})
}

// Regression: a 64-character input that normalises to 65 characters used to be
// accepted although its own String() could not be parsed again.
func TestAmountRoundTripsAtTheLengthLimit(t *testing.T) {
	for _, in := range []string{
		"." + strings.Repeat("0", 62), // 64 chars in, "0." + 62 zeros = 64 out
		"." + strings.Repeat("0", 63), // 64 chars in, 65 out: must be rejected
		strings.Repeat("9", 64),       // exactly at the limit
		strings.Repeat("9", 65),       // one over
		"1" + strings.Repeat("0", 63), // 64 digits
		"1e63",                        // expands to 64 digits
		"1e64",                        // expands to 65 digits
	} {
		a, err := ParseAmount(in)
		if err != nil {
			continue
		}
		if b, err := ParseAmount(a.String()); err != nil || b.String() != a.String() {
			t.Errorf("%q accepted as %q but it does not round-trip: %v", in, a, err)
		}
	}
}
