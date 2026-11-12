package http

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net"
	gohttp "net/http"
	"strconv"
	"syscall"
	"time"
)

type RetryOn uint8

const (
	RetryOnBusy RetryOn = 1 << iota
	// RetryOnGatewayError covers 502 and 504, after which the server may have acted on the request.
	RetryOnGatewayError
	RetryOnBrokenConnection
	RetryOnLocked

	RetryOnTransient = RetryOnBusy | RetryOnGatewayError | RetryOnBrokenConnection | RetryOnLocked
)

type retryOptions struct {
	policy     func(method string) RetryOn
	maxRetries int
	backoff    retryBackoff
}

func (on RetryOn) backoff(resp *gohttp.Response, err error, backoff retryBackoff) retryBackoff {
	isBrokenConnection := func(err error) bool {
		return errors.Is(err, syscall.ECONNRESET) || errors.Is(err, syscall.EPIPE) ||
			errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, net.ErrClosed)
	}
	switch {
	case err != nil:
		// A refused connection is retried under every policy, because the server received nothing.
		if errors.Is(err, syscall.ECONNREFUSED) || on&RetryOnBrokenConnection != 0 && isBrokenConnection(err) {
			return backoff
		}
		return nil
	case on&RetryOnGatewayError != 0 && (resp.StatusCode == gohttp.StatusBadGateway || resp.StatusCode == gohttp.StatusGatewayTimeout):
		return backoff
	case on&RetryOnBusy != 0 && (resp.StatusCode == gohttp.StatusTooManyRequests || resp.StatusCode == gohttp.StatusServiceUnavailable):
		return retryAfterBackoff{Response: resp, Fallback: backoff}
	case on&RetryOnLocked != 0 && resp.StatusCode == gohttp.StatusLocked:
		return backoff
	default:
		return nil
	}
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
	next gohttp.RoundTripper
}

func (r retryRoundTripper) RoundTrip(req *gohttp.Request) (*gohttp.Response, error) {
	retryOpts := requestOptionsFrom(req).retry
	on := retryOpts.policy(req.Method)
	req = makeRequestBodyRetryable(req)
	for attempt := 1; ; attempt++ {
		resp, err := r.next.RoundTrip(req)
		// A request the context ended is not retried. The context is checked rather than the error,
		// since the transport returns the context's cause, which for Ctrl-C is a signal error that
		// wraps no context.Canceled.
		if errors.Is(err, errRetryableBodyClose) || err != nil && req.Context().Err() != nil {
			return resp, err
		}

		backoff := on.backoff(resp, err, retryOpts.backoff)
		if backoff == nil || attempt > retryOpts.maxRetries {
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
			"attempt", fmt.Sprintf("%d/%d", attempt, retryOpts.maxRetries),
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

const maxDrainBytes = 16 * 1024

func drainAndCloseResponseBody(ctx context.Context, resp *gohttp.Response) {
	if resp != nil && resp.Body != nil {
		drainedBytes, err := io.CopyN(io.Discard, resp.Body, maxDrainBytes)
		if err != nil && !errors.Is(err, io.EOF) {
			slog.DebugContext(ctx, "failed to drain response body: "+err.Error())
		}
		if err := resp.Body.Close(); err != nil {
			slog.DebugContext(ctx, fmt.Sprintf("failed to close response body after draining %d bytes: %s", drainedBytes, err.Error()))
		}
	}
}
