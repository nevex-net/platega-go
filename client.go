package platega

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// DefaultBaseURL is the production Platega API endpoint.
const DefaultBaseURL = "https://app.platega.io"

const (
	defaultTimeout    = 30 * time.Second
	defaultRetryWait  = 300 * time.Millisecond
	maxRetryWait      = 30 * time.Second
	maxResponseBytes  = 10 << 20
	defaultUserAgent  = "platega-go"
	headerMerchantID  = "X-MerchantId"
	headerSecret      = "X-Secret"
	headerIdempotency = "Idempotency-Key"
)

// Client is a Platega API client. It is safe for concurrent use.
type Client struct {
	baseURL      *url.URL
	merchantID   string
	secret       string
	payoutSecret string
	httpClient   *http.Client
	userAgent    string
	maxRetries   int
	retryWait    time.Duration
	logger       *slog.Logger
	now          func() time.Time
}

// Option configures a [Client].
type Option func(*clientConfig)

type clientConfig struct {
	baseURL      string
	payoutSecret string
	httpClient   *http.Client
	userAgent    string
	maxRetries   int
	retryWait    time.Duration
	logger       *slog.Logger
	now          func() time.Time
}

// WithBaseURL overrides the API base URL (for a sandbox or a test server).
func WithBaseURL(u string) Option { return func(c *clientConfig) { c.baseURL = u } }

// WithHTTPClient sets the HTTP client used for all requests. Wrap its
// Transport to add tracing, metrics or custom TLS settings.
func WithHTTPClient(h *http.Client) Option { return func(c *clientConfig) { c.httpClient = h } }

// WithUserAgent sets a custom User-Agent header.
func WithUserAgent(ua string) Option { return func(c *clientConfig) { c.userAgent = ua } }

// WithLogger makes the client log every request attempt to l: Debug for each
// attempt and Warn when it is about to retry. A nil logger, the default,
// disables logging.
//
// Only the method, path (without the query string), status, duration, attempt
// number and error are logged. Credentials, headers and bodies never are.
func WithLogger(l *slog.Logger) Option { return func(c *clientConfig) { c.logger = l } }

// WithPayoutSecret sets the secret used to sign Payout API requests (saved
// cards and payouts) with HMAC-SHA256. By default the merchant secret passed
// to [New] is used, which is what Platega's own SDKs do; set this if your
// Payout API key is a different one.
func WithPayoutSecret(s string) Option { return func(c *clientConfig) { c.payoutSecret = s } }

// WithRetry enables retries for idempotent GET requests that fail with a
// network error or a 429/502/503/504 response. The wait doubles after each
// attempt (up to 30 seconds) with jitter, and a Retry-After header from the
// server takes precedence. Requests that create or change state are never
// retried.
func WithRetry(maxRetries int, wait time.Duration) Option {
	return func(c *clientConfig) {
		c.maxRetries = maxRetries
		c.retryWait = wait
	}
}

// New creates a client for the given merchant credentials.
func New(merchantID, secret string, opts ...Option) (*Client, error) {
	if merchantID == "" || secret == "" {
		return nil, errors.New("platega: merchantID and secret are required")
	}
	cfg := clientConfig{
		baseURL:   DefaultBaseURL,
		userAgent: defaultUserAgent,
		retryWait: defaultRetryWait,
		now:       time.Now,
	}
	for _, o := range opts {
		o(&cfg)
	}
	u, err := url.Parse(strings.TrimRight(cfg.baseURL, "/"))
	if err != nil || u.Scheme == "" || u.Host == "" {
		return nil, fmt.Errorf("platega: invalid base URL %q", cfg.baseURL)
	}
	hc := cfg.httpClient
	if hc == nil {
		hc = &http.Client{Timeout: defaultTimeout}
	}
	payoutSecret := cfg.payoutSecret
	if payoutSecret == "" {
		payoutSecret = secret
	}
	return &Client{
		baseURL:      u,
		merchantID:   merchantID,
		secret:       secret,
		payoutSecret: payoutSecret,
		httpClient:   hc,
		userAgent:    cfg.userAgent,
		maxRetries:   cfg.maxRetries,
		retryWait:    cfg.retryWait,
		logger:       cfg.logger,
		now:          cfg.now,
	}, nil
}

type authKind int

const (
	authKey  authKind = iota // X-MerchantId + X-Secret headers
	authHMAC                 // Authorization: PG-HMAC ... (Payout API)
)

type request struct {
	method         string
	path           string
	query          url.Values
	body           any
	auth           authKind
	idempotencyKey string
	// accept overrides the default "Accept: application/json".
	accept string
}

type response struct {
	status int
	header http.Header
	body   []byte
}

func (c *Client) do(ctx context.Context, r request, out any) error {
	var body []byte
	if r.body != nil {
		// json.Marshal output is compact, which matters for HMAC: the exact
		// bytes that are signed are the bytes that are sent.
		b, err := json.Marshal(r.body)
		if err != nil {
			return fmt.Errorf("platega: encode request: %w", err)
		}
		body = b
	}

	// r.path is already escaped (see pathID); set Path and RawPath together so
	// url.URL does not escape it a second time.
	u := *c.baseURL
	decoded, err := url.PathUnescape(r.path)
	if err != nil {
		return fmt.Errorf("platega: invalid request path %q: %w", r.path, err)
	}
	basePath := strings.TrimRight(u.EscapedPath(), "/")
	u.Path = strings.TrimRight(u.Path, "/") + decoded
	u.RawPath = basePath + r.path
	// The Payout API signs the path together with the query string, exactly as
	// it is sent.
	signedPath := r.path
	if len(r.query) > 0 {
		u.RawQuery = r.query.Encode()
		signedPath += "?" + u.RawQuery
	}

	retriable := r.method == http.MethodGet
	wait := c.retryWait
	for attempt := 1; ; attempt++ {
		start := time.Now()
		resp, err := c.send(ctx, r, u.String(), signedPath, body)
		c.logAttempt(ctx, r, attempt, resp.status, time.Since(start), err)

		if retriable && attempt <= c.maxRetries && shouldRetry(resp.status, err) {
			delay := retryDelay(wait, resp.header, c.now())
			c.logRetry(ctx, r, attempt, delay)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(delay):
			}
			wait = min(wait*2, maxRetryWait)
			continue
		}
		if err != nil {
			return err
		}
		if resp.status < 200 || resp.status > 299 {
			return newAPIError(r, resp)
		}
		if out == nil || len(bytes.TrimSpace(resp.body)) == 0 {
			return nil
		}
		if err := json.Unmarshal(resp.body, out); err != nil {
			return fmt.Errorf("platega: decode %s %s response: %w", r.method, r.path, err)
		}
		return nil
	}
}

func (c *Client) send(ctx context.Context, r request, target, signedPath string, body []byte) (response, error) {
	var rd io.Reader
	if body != nil {
		rd = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, r.method, target, rd)
	if err != nil {
		return response{}, fmt.Errorf("platega: build request: %w", err)
	}
	accept := r.accept
	if accept == "" {
		accept = "application/json"
	}
	req.Header.Set("Accept", accept)
	req.Header.Set("User-Agent", c.userAgent)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	switch r.auth {
	case authHMAC:
		ts := c.now().Unix()
		sig := signPG(c.payoutSecret, r.method, signedPath, ts, r.idempotencyKey, body)
		req.Header.Set("Authorization",
			"PG-HMAC kid="+c.merchantID+", ts="+strconv.FormatInt(ts, 10)+", sig="+sig)
		if r.idempotencyKey != "" {
			req.Header.Set(headerIdempotency, r.idempotencyKey)
		}
	default:
		req.Header.Set(headerMerchantID, c.merchantID)
		req.Header.Set(headerSecret, c.secret)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return response{}, fmt.Errorf("platega: %s %s: %w", r.method, r.path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	b, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return response{status: resp.StatusCode, header: resp.Header}, fmt.Errorf("platega: read response: %w", err)
	}
	return response{status: resp.StatusCode, header: resp.Header, body: b}, nil
}

func (c *Client) logAttempt(ctx context.Context, r request, attempt, status int, d time.Duration, err error) {
	if c.logger == nil {
		return
	}
	attrs := []slog.Attr{
		slog.String("method", r.method),
		slog.String("path", r.path),
		slog.Int("attempt", attempt),
		slog.Duration("duration", d),
	}
	if status != 0 {
		attrs = append(attrs, slog.Int("status", status))
	}
	if err != nil {
		attrs = append(attrs, slog.String("error", err.Error()))
	}
	c.logger.LogAttrs(ctx, slog.LevelDebug, "platega request", attrs...)
}

func (c *Client) logRetry(ctx context.Context, r request, attempt int, delay time.Duration) {
	if c.logger == nil {
		return
	}
	c.logger.LogAttrs(ctx, slog.LevelWarn, "platega request will be retried",
		slog.String("method", r.method),
		slog.String("path", r.path),
		slog.Int("attempt", attempt),
		slog.Duration("delay", delay),
	)
}

func shouldRetry(status int, err error) bool {
	if err != nil {
		return true
	}
	switch status {
	case http.StatusTooManyRequests, http.StatusBadGateway,
		http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return true
	}
	return false
}

// retryDelay picks how long to wait before the next attempt: the server's
// Retry-After if it sent one (capped), otherwise the backoff with jitter so
// that many clients do not retry in lockstep.
func retryDelay(backoff time.Duration, h http.Header, now time.Time) time.Duration {
	if d, ok := parseRetryAfter(h.Get("Retry-After"), now); ok {
		return min(d, maxRetryWait)
	}
	if backoff <= 0 {
		return 0
	}
	half := backoff / 2
	//nolint:gosec // jitter does not need cryptographic randomness
	return half + time.Duration(rand.Int64N(int64(half)+1))
}

// parseRetryAfter understands both forms of the header: delay-seconds and an
// HTTP date.
func parseRetryAfter(v string, now time.Time) (time.Duration, bool) {
	v = strings.TrimSpace(v)
	if v == "" {
		return 0, false
	}
	if secs, err := strconv.Atoi(v); err == nil {
		if secs < 0 {
			return 0, false
		}
		return time.Duration(secs) * time.Second, true
	}
	if t, err := http.ParseTime(v); err == nil {
		return max(t.Sub(now), 0), true
	}
	return 0, false
}

// signPG builds the Payout API signature:
//
//	Base64(HMAC-SHA256(secret, METHOD \n PATH \n ts \n idempotency-key \n sha256hex(body)))
//
// PATH includes the query string.
func signPG(secret, method, path string, ts int64, idempotencyKey string, body []byte) string {
	sum := sha256.Sum256(body)
	msg := strings.Join([]string{
		method, path, strconv.FormatInt(ts, 10), idempotencyKey, hex.EncodeToString(sum[:]),
	}, "\n")
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(msg))
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

// pathID escapes a user-supplied identifier for use as a URL path segment.
func pathID(id string) (string, error) {
	if id == "" {
		return "", errors.New("platega: id must not be empty")
	}
	return url.PathEscape(id), nil
}
