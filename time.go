package platega

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Time is a time.Time that tolerates the empty and null values the API uses
// for "not set". The zero value means not set.
type Time struct{ time.Time }

// UnmarshalJSON implements json.Unmarshaler.
func (t *Time) UnmarshalJSON(b []byte) error {
	s := strings.Trim(string(bytes.TrimSpace(b)), `"`)
	if s == "" || s == "null" {
		t.Time = time.Time{}
		return nil
	}
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02 15:04:05"} {
		if v, err := time.Parse(layout, s); err == nil {
			t.Time = v
			return nil
		}
	}
	return fmt.Errorf("platega: unrecognised time %q", s)
}

// MarshalJSON implements json.Marshaler.
func (t Time) MarshalJSON() ([]byte, error) {
	if t.IsZero() {
		return []byte("null"), nil
	}
	return json.Marshal(t.Format(time.RFC3339Nano))
}

// ParseExpiresIn converts the API's "HH:MM:SS" (optionally "D.HH:MM:SS")
// time-to-live into a duration.
func ParseExpiresIn(s string) (time.Duration, error) {
	orig := s
	var days int
	if i := strings.IndexByte(s, '.'); i >= 0 && strings.Count(s, ":") == 2 && i < strings.IndexByte(s, ':') {
		d, err := strconv.Atoi(s[:i])
		if err != nil {
			return 0, fmt.Errorf("platega: invalid duration %q", orig)
		}
		days, s = d, s[i+1:]
	}
	parts := strings.Split(s, ":")
	if len(parts) != 3 {
		return 0, fmt.Errorf("platega: invalid duration %q", orig)
	}
	var n [3]int
	for i, p := range parts {
		v, err := strconv.Atoi(p)
		if err != nil || v < 0 {
			return 0, fmt.Errorf("platega: invalid duration %q", orig)
		}
		n[i] = v
	}
	return time.Duration(days)*24*time.Hour +
		time.Duration(n[0])*time.Hour +
		time.Duration(n[1])*time.Minute +
		time.Duration(n[2])*time.Second, nil
}
