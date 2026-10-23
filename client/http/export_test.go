package http

func WithTestRetry(c Client, maxRetries int, backoff retryBackoff) Client {
	c.Transport = retryOptions{policy: defaultRetryPolicy, maxRetries: maxRetries, backoff: backoff}.wrap(c.Transport)
	return c
}
