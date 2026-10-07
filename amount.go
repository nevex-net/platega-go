package platega

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"strconv"
	"strings"
)

const (
	// maxAmountLen bounds the normalised form. Checking the result, not the
	// input, keeps every accepted amount re-parsable from its own String().
	maxAmountLen = 64
	// maxAmountInput only guards against absurdly long input.
	maxAmountInput    = 256
	maxAmountExponent = 40
)

// Amount is an exact decimal number: a fiat sum, a crypto amount or an
// exchange rate.
//
// Platega returns amounts as JSON numbers, some with many decimals (USDT
// values such as 0.01236094). Decoding those into float64 loses exactness and
// makes comparisons unreliable, so Amount keeps the digits exactly as they
// were sent and only converts on request.
//
// The zero value means "not set" (a missing or null field), like the zero
// time.Time; use [Amount.Sign] to test for the numeric value zero. Amounts
// are values: compare them with [Amount.Equal] or [Amount.Cmp], never with ==,
// because 500 and 500.00 are the same number but different strings.
type Amount struct{ s string }

// ParseAmount parses a decimal number such as "500", "0.01236094", "-1.5" or
// "1e-7". Exponent notation is expanded to plain digits.
func ParseAmount(s string) (Amount, error) {
	plain, err := normalizeDecimal(strings.TrimSpace(s))
	if err != nil {
		return Amount{}, fmt.Errorf("platega: invalid amount %q: %w", s, err)
	}
	return Amount{plain}, nil
}

// MustAmount is like [ParseAmount] but panics on invalid input. It is meant
// for constants and tests.
func MustAmount(s string) Amount {
	a, err := ParseAmount(s)
	if err != nil {
		panic(err)
	}
	return a
}

// AmountFromInt returns the exact amount for a whole number.
func AmountFromInt(n int64) Amount { return Amount{strconv.FormatInt(n, 10)} }

// String returns the amount as plain decimal digits, or "" if it is not set.
func (a Amount) String() string { return a.s }

// IsZero reports whether the amount is not set (missing or null in JSON). It
// is not the numeric zero: see [Amount.Sign].
func (a Amount) IsZero() bool { return a.s == "" }

// Sign returns -1, 0 or +1 for a negative, zero or positive amount. An unset
// amount has sign 0.
func (a Amount) Sign() int { return a.Rat().Sign() }

// Cmp compares two amounts numerically and returns -1, 0 or +1. An unset
// amount compares as zero.
func (a Amount) Cmp(b Amount) int { return a.Rat().Cmp(b.Rat()) }

// Equal reports whether two amounts are numerically equal, so 500 equals
// 500.00. An unset amount equals zero.
func (a Amount) Equal(b Amount) bool { return a.Cmp(b) == 0 }

// Rat returns the exact value as a new big.Rat, for arithmetic. An unset
// amount returns 0.
func (a Amount) Rat() *big.Rat {
	r := new(big.Rat)
	if a.s == "" {
		return r
	}
	// a.s was validated on construction.
	r.SetString(a.s)
	return r
}

// Float64 returns the nearest float64. It may be inexact; use it for display
// or approximate maths only, never for money comparisons.
func (a Amount) Float64() float64 {
	f, _ := a.Rat().Float64()
	return f
}

// MarshalJSON encodes the amount as a JSON number, or null if it is not set.
func (a Amount) MarshalJSON() ([]byte, error) {
	if a.s == "" {
		return []byte("null"), nil
	}
	return []byte(a.s), nil
}

// UnmarshalJSON decodes a JSON number, or a string holding a number, or null.
func (a *Amount) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	if bytes.Equal(b, []byte("null")) {
		*a = Amount{}
		return nil
	}
	s := string(b)
	if len(b) > 0 && b[0] == '"' {
		if err := json.Unmarshal(b, &s); err != nil {
			return err
		}
	}
	v, err := ParseAmount(s)
	if err != nil {
		return err
	}
	*a = v
	return nil
}

// normalizeDecimal validates s and rewrites it as plain decimal digits
// without an exponent or a redundant sign, keeping the fractional digits it
// was given ("500.00" stays "500.00").
func normalizeDecimal(s string) (string, error) {
	if s == "" {
		return "", errors.New("empty")
	}
	if len(s) > maxAmountInput {
		return "", errors.New("too long")
	}
	i, neg := 0, false
	switch s[0] {
	case '-':
		neg, i = true, 1
	case '+':
		i = 1
	}
	digits := func() string {
		start := i
		for i < len(s) && s[i] >= '0' && s[i] <= '9' {
			i++
		}
		return s[start:i]
	}
	intPart := digits()
	frac := ""
	if i < len(s) && s[i] == '.' {
		i++
		frac = digits()
	}
	if intPart == "" && frac == "" {
		return "", errors.New("no digits")
	}
	exp := 0
	if i < len(s) && (s[i] == 'e' || s[i] == 'E') {
		i++
		expStart := i
		if i < len(s) && (s[i] == '+' || s[i] == '-') {
			i++
		}
		digitsStart := i
		for i < len(s) && s[i] >= '0' && s[i] <= '9' {
			i++
		}
		if digitsStart == i {
			return "", errors.New("bad exponent")
		}
		e, err := strconv.Atoi(s[expStart:i])
		if err != nil || e > maxAmountExponent || e < -maxAmountExponent {
			return "", errors.New("exponent out of range")
		}
		exp = e
	}
	if i != len(s) {
		return "", errors.New("unexpected character")
	}

	all := intPart + frac
	point := len(intPart) + exp
	if point < 0 {
		all, point = strings.Repeat("0", -point)+all, 0
	} else if point > len(all) {
		all += strings.Repeat("0", point-len(all))
	}
	ip, fp := strings.TrimLeft(all[:point], "0"), all[point:]
	if ip == "" {
		ip = "0"
	}
	out := ip
	if fp != "" {
		out += "." + fp
	}
	if neg && strings.Trim(out, "0.") != "" {
		out = "-" + out
	}
	if len(out) > maxAmountLen {
		return "", errors.New("too many digits")
	}
	return out, nil
}
