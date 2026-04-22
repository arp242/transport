package transport

import (
	"context"
	"io"
	"net/http"
	"strconv"
	"time"
)

type retry struct {
	parent  http.RoundTripper
	wait    func(int, *http.Response, error) time.Duration
	timeout time.Duration
}

var retryTesthook func(i int, resp *http.Response, err error, d time.Duration)

type cancelCloser struct {
	io.ReadCloser
	cancel func()
}

func (c cancelCloser) Close() error { c.cancel(); return c.ReadCloser.Close() }

var _ io.ReadCloser = cancelCloser{}

func (t retry) RoundTrip(r *http.Request) (*http.Response, error) {
	var (
		i   int
		ctx = r.Context()
	)
	for {
		var (
			ctx2   context.Context
			cancel context.CancelFunc
		)
		if t.timeout > 0 {
			ctx2, cancel = context.WithTimeout(ctx, t.timeout)
			r = r.Clone(ctx2)
		}

		resp, err := t.parent.RoundTrip(r)
		if resp != nil && resp.Body != nil && cancel != nil {
			// Call cancel() on resp.Body.Close instead of defer, since that can
			// be called too soon and http.Client will pick up on it.
			//
			// TODO: doesn't show in tests as they're too fast(?) Should write a
			// test for it.
			resp.Body = cancelCloser{resp.Body, cancel}
		}
		if err == nil && resp.StatusCode < 400 {
			return resp, err
		}

		d := t.wait(i, resp, err)
		if retryTesthook != nil {
			retryTesthook(i, resp, err, d)
		}
		if d < 0 {
			if resp == nil && cancel != nil {
				cancel()
			}
			return resp, err
		}
		time.Sleep(d)

		if resp != nil && resp.Body != nil {
			resp.Body.Close()
		}
		if cancel != nil {
			cancel()
		}
		i++
	}
}

// Retry on any network error or any HTTP status >=400.
//
// It calls the provided callback to determine how long too wait after an error,
// retrying immediately if the returned duration is 0, or aborting if it's <0.
//
// The [http.Client.Timeout] applies to the entire request chain (including all
// retries). The timeout parameter sets a timeout for every retry attempt. The
// timeout applies to the request only, not the waiting period. <=0 means there
// is no additional timeout.
//
// The [RetryRatelimit] helper can be used to delay until the ratelimit resets.
func Retry(
	parent http.RoundTripper,
	timeout time.Duration,
	wait func(i int, resp *http.Response, err error) time.Duration,
) *retry {
	return &retry{parent, wait, timeout}
}

// RetryRatelimit tries to determine the time to wait until the ratelimit resets.
//
// other is used as the default if err != nil, if the status isn't 429 or 503,
// if there are no rate limit headers, or if there is any error parsing the
// headers.
func RetryRatelimit(other time.Duration) func(_ int, resp *http.Response, err error) time.Duration {
	return func(_ int, resp *http.Response, err error) time.Duration {
		if err != nil {
			return other
		}
		if resp.StatusCode == 429 || resp.StatusCode == 503 {
			if after := resp.Header.Get("Retry-After"); after != "" {
				i, err := strconv.ParseInt(after, 10, 64)
				if err == nil {
					return time.Duration(i) * time.Second
				}
				t, err := time.Parse(time.RFC1123, after)
				if err == nil {
					return max(time.Until(t), 0)
				}
			}
		}
		if resp.StatusCode == 429 {
			for _, h := range []string{"X-Ratelimit-Reset", "Ratelimit-Reset", "X-Rate-Limit-Reset"} {
				i, err := strconv.ParseInt(resp.Header.Get(h), 10, 64)
				if err == nil {
					return time.Duration(i) * time.Second
				}

			}
		}
		return other
	}
}
