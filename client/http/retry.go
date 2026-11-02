package http

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	gohttp "net/http"
	"strconv"
	"syscall"
	"time"
)

type RetryOn uint8

const (
	// RetryOnBusy waits as long as the Retry-After of a 429 or 503 asks.
	RetryOnBusy RetryOn = 1 << iota
	// RetryOnGatewayError covers 502 and 504, after which the server may have acted on the request.
	RetryOnGatewayError
	// RetryOnBrokenConnection covers a connection that broke once it was up, as one an ingress drops
	// during a rolling deploy does.
	RetryOnBrokenConnection

	RetryOnTransient = RetryOnBusy | RetryOnGatewayError | RetryOnBrokenConnection
)

// RetryPolicy gets retryable true when the caller marked the request with [Retryable].
type RetryPolicy func(method string, retryable bool) RetryOn

func defaultRetryPolicy(method string, retryable bool) RetryOn {
	if method == MethodGet || retryable {
		return RetryOnTransient
	}
	return 0
}

type retryOptions struct {
	policy     RetryPolicy
	maxRetries int
	backoff    retryBackoff
}

func (options retryOptions) wrap(next gohttp.RoundTripper) gohttp.RoundTripper {
	return &retryRoundTripper{next: next, retryOptions: options}
}

func (on RetryOn) backoff(resp *gohttp.Response, err error, fallback retryBackoff) retryBackoff {
	switch {
	case err != nil:
		// An unknown host, a refused connection or a TLS error repeats on every attempt, and a
		// timeout would multiply by the retries.
		if on&RetryOnBrokenConnection != 0 && isBrokenConnection(err) {
			return fallback
		}
	case resp.StatusCode == gohttp.StatusTooManyRequests || resp.StatusCode == gohttp.StatusServiceUnavailable:
		if on&RetryOnBusy != 0 {
			return retryAfterBackoff{Response: resp, Fallback: fallback}
		}
	case resp.StatusCode == gohttp.StatusBadGateway || resp.StatusCode == gohttp.StatusGatewayTimeout:
		if on&RetryOnGatewayError != 0 {
			return fallback
		}
	}
	return nil
}

func isBrokenConnection(err error) bool {
	return errors.Is(err, syscall.ECONNRESET) || errors.Is(err, syscall.EPIPE) ||
		errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF)
}

type retryBackoff interface {
	Calculate(attempt int) time.Duration
}

type exponentialBackoff struct {
	MinWait, MaxWait time.Duration
}

func (b exponentialBackoff) Calculate(attempt int) time.Duration {
	nextWait := time.Duration(math.Pow(2, float64(attempt-1))) * b.MinWait
	if b.MaxWait > 0 && nextWait > b.MaxWait {
		return b.MaxWait
	}
	return nextWait
}

var timeNow = time.Now

type retryAfterBackoff struct {
	Response *gohttp.Response
	Fallback retryBackoff
}

func (b retryAfterBackoff) Calculate(attempt int) (waitTime time.Duration) {
	defer func() {
		const maxRetryAfterWaitTime = 5 * time.Minute
		if waitTime < 0 {
			waitTime = b.Fallback.Calculate(attempt)
		} else if waitTime > maxRetryAfterWaitTime {
			waitTime = maxRetryAfterWaitTime
		}
	}()

	// Retry-After holds either delay-seconds or an HTTP-date, RFC 7231 §7.1.3.
	header := b.Response.Header.Get("Retry-After")
	if header == "" {
		return -1
	}

	if seconds, err := strconv.ParseInt(header, 10, 64); err == nil {
		return time.Duration(seconds) * time.Second
	}

	if date, err := gohttp.ParseTime(header); err == nil {
		return date.Sub(timeNow())
	}
	return -1
}

type retryRoundTripper struct {
	retryOptions

	next gohttp.RoundTripper
}

func (r *retryRoundTripper) RoundTrip(req *gohttp.Request) (*gohttp.Response, error) {
	on := r.policy(req.Method, isRetryable(req))
	if on == 0 {
		return r.next.RoundTrip(req)
	}
	req = makeRequestBodyRetryable(req)
	for attempt := 1; ; attempt++ {
		resp, err := r.next.RoundTrip(req)
		// A request the context ended is not retried. The context is checked rather than the error,
		// since the transport returns the context's cause, which for Ctrl-C is a signal error that
		// wraps no context.Canceled.
		if errors.Is(err, errRetryableBodyClose) || err != nil && req.Context().Err() != nil {
			return resp, err
		}
		backoff := on.backoff(resp, err, r.backoff)
		if backoff == nil || attempt > r.maxRetries {
			return resp, err
		}
		drainAndCloseResponseBody(req.Context(), resp)
		if req.GetBody != nil {
			body, bodyErr := req.GetBody()
			if bodyErr != nil {
				return nil, errors.Join(err, bodyErr)
			}
			req.Body = body
		}
		waitTime := backoff.Calculate(attempt)
		slog.WarnContext(req.Context(), "retrying request", append(
			func() []any {
				if err != nil {
					return []any{"error", err.Error()}
				}
				return []any{"status", resp.StatusCode}
			}(),
			"method", req.Method,
			"path", req.URL.Path,
			"attempt", fmt.Sprintf("%d/%d", attempt, r.maxRetries),
			"waitTime", waitTime,
		)...)
		timer := time.NewTimer(waitTime)
		select {
		case <-req.Context().Done():
			timer.Stop()
			return nil, req.Context().Err()
		case <-timer.C:
		}
	}
}

func makeRequestBodyRetryable(req *gohttp.Request) *gohttp.Request {
	if req.Body == nil {
		return req
	}
	// gohttp.NewRequestWithContext sets GetBody for a *bytes.Buffer, *bytes.Reader or
	// *strings.Reader body.
	if req.GetBody != nil {
		return req
	}
	body := retryableBody{Closer: req.Body}
	body.Reader = io.TeeReader(req.Body, &body.Buffer)
	result := req.Clone(req.Context())
	result.Body = &body
	result.GetBody = nil
	return result
}

type retryableBody struct {
	io.Reader
	io.Closer

	Buffer appendWriter
}

var errRetryableBodyClose = errors.New("retryableBody failed to close")

func (b *retryableBody) Close() error {
	// The transport can stop reading early, for example on a connection reset, and the retry
	// replays Buffer, so Buffer needs the rest of the body.
	if _, err := io.Copy(io.Discard, b.Reader); err != nil {
		return errors.Join(err, errRetryableBodyClose)
	}
	if b.Closer != nil {
		if err := b.Closer.Close(); err != nil {
			return errors.Join(err, errRetryableBodyClose)
		}
	}
	b.Closer = nil
	b.Reader = bytes.NewReader(b.Buffer)
	return nil
}

type appendWriter []byte

func (w *appendWriter) Write(p []byte) (int, error) {
	*w = append(*w, p...)
	return len(p), nil
}

// drainAndCloseResponseBody drains the body so that gohttp.Transport can reuse the connection.
// It reads at most maxBytes, so that a large or slow body cannot block the retry; the connection
// of such a body is then not reused.
func drainAndCloseResponseBody(ctx context.Context, resp *gohttp.Response) {
	const maxBytes = 16 * 1024
	if resp != nil && resp.Body != nil {
		drainedBytes, err := io.CopyN(io.Discard, resp.Body, maxBytes)
		if err != nil && !errors.Is(err, io.EOF) {
			slog.DebugContext(ctx, "failed to drain response body: "+err.Error())
		}
		if err := resp.Body.Close(); err != nil {
			slog.DebugContext(ctx, fmt.Sprintf("failed to close response body after draining %d bytes: %s", drainedBytes, err.Error()))
		}
	}
}
