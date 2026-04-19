package transport

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync"
	"testing"
	"time"
)

// TODO: can speed up tests by running in parallel, but we need to rewrite it by
// passing test/check state through the headers, context value, or something
// instead of relying on globals. I can't be bothered to rewrite it now though.

type alwayserr struct{}

func (e alwayserr) RoundTrip(r *http.Request) (*http.Response, error) {
	return nil, errors.New("RoundTrip error")
}

func TestRetry(t *testing.T) {
	var i int
	tests := []struct {
		srv               func(http.ResponseWriter, *http.Request)
		transport         http.RoundTripper
		wantStatus        int
		wantBody, wantErr string
		wantFails         []string
	}{
		{
			func(w http.ResponseWriter, r *http.Request) {
				switch i {
				case 0:
					w.WriteHeader(500)
				case 1:
					w.WriteHeader(503)
				case 2:
					w.WriteHeader(400)
				case 3:
					w.WriteHeader(404)
				case 4:
					w.Header().Set("Retry-After", "1")
					w.WriteHeader(503)
				case 5:
					w.Header().Set("X-Ratelimit-Reset", "1")
					w.WriteHeader(429)
				// Annoying to test as wait time is variable.
				//case 6:
				//	w.Header().Set("Retry-After", time.Now().Add(time.Second).Format(time.RFC1123))
				//	w.WriteHeader(503)
				default:
					w.Write([]byte("HANDLE"))
				}
			},
			Retry(http.DefaultTransport, 0, func(i int, resp *http.Response, err error) time.Duration {
				if err != nil {
					return -1
				}
				return RetryRatelimit(0)(i, resp, err)
			}),
			200, "HANDLE", "", []string{
				"0 <nil> 500 0s",
				"1 <nil> 503 0s",
				"2 <nil> 400 0s",
				"3 <nil> 404 0s",
				"4 <nil> 503 1s",
				"5 <nil> 429 1s",
			},
		},

		{
			func(w http.ResponseWriter, r *http.Request) {
				switch i {
				case 0:
					// Make sure it doesn't return a negative duration.
					w.Header().Set("Retry-After", time.Now().Add(-time.Second).Format(time.RFC1123))
					w.WriteHeader(503)
				default:
					w.Write([]byte("HANDLE"))
				}
			},
			Retry(http.DefaultTransport, 0, func(i int, resp *http.Response, err error) time.Duration {
				return RetryRatelimit(0)(i, resp, err)
			}),
			200, "HANDLE", "", []string{
				"0 <nil> 503 0s",
			},
		},

		{
			func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(500 + i)
				fmt.Fprintf(w, "ERR %d", i)
			},
			Retry(http.DefaultTransport, 0, func(i int, resp *http.Response, err error) time.Duration {
				if i > 1 {
					return -1
				}
				return 0
			}),
			502, "ERR 2", "", []string{
				"0 <nil> 500 0s",
				"1 <nil> 501 0s",
				"2 <nil> 502 -1ns",
			},
		},

		{
			func(w http.ResponseWriter, r *http.Request) {
				w.Write([]byte("HANDLE"))
			},
			Retry(alwayserr{}, 0, func(i int, resp *http.Response, err error) time.Duration {
				if i > 1 {
					return -1
				}
				return RetryRatelimit(0)(i, resp, err)
			}),
			200, "HANDLE", "RoundTrip error", []string{
				"0 <nil> 503 0s",
			},
		},

		{ // Make sure it doesn't re-use the timeout between requests.
			func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(500 + i)
				fmt.Fprintf(w, "ERR %d", i)
			},
			Retry(http.DefaultTransport, 900*time.Millisecond, func(i int, resp *http.Response, err error) time.Duration {
				if i > 2 {
					return -1
				}
				return 1 * time.Second
			}),
			503, "ERR 3", "", []string{
				"0 <nil> 500 1s",
				"1 <nil> 501 1s",
				"2 <nil> 502 1s",
				"3 <nil> 503 -1ns",
			},
		},
	}

	for _, tt := range tests {
		t.Run("", func(t *testing.T) {
			i = 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { tt.srv(w, r); i++ }))
			t.Cleanup(srv.Close)

			fails := make([]string, 0, 4)
			retryTesthook = func(i int, resp *http.Response, err error, d time.Duration) {
				code := 0
				if resp != nil {
					code = resp.StatusCode
				}
				fails = append(fails, fmt.Sprintf("%d %v %d %s", i, err, code, d))
			}
			t.Cleanup(func() { retryTesthook = nil })

			c := http.Client{Transport: tt.transport}
			resp, err := c.Get(srv.URL)
			if !errorContains(err, tt.wantErr) {
				t.Fatalf("wrong error\nhave: %v\nwant: %v\nfails: %#v", err, tt.wantErr, fails)
			}
			if tt.wantErr != "" {
				if resp != nil {
					t.Fatalf("resp is not nil: %v", resp)
				}
				return
			}
			defer resp.Body.Close()

			if resp.StatusCode != tt.wantStatus {
				t.Fatalf("wrong status\nhave: %d\nwant: %d", resp.StatusCode, tt.wantStatus)
			}
			b, err := io.ReadAll(resp.Body)
			if err != nil {
				t.Fatal(err)
			}
			if string(b) != tt.wantBody {
				t.Fatalf("wrong body\nhave: %q\nwant: %q", string(b), tt.wantBody)
			}
			if !reflect.DeepEqual(fails, tt.wantFails) {
				t.Errorf("\nhave: %#v\nwant: %#v", fails, tt.wantFails)
			}
		})
	}
}

// Timeout applies to entire request chain, not single instance.
func TestRetry_Timeout(t *testing.T) {
	var i int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if i < 2 {
			w.WriteHeader(503)
		}
		w.Write([]byte("HANDLE"))
		i++
	}))
	t.Cleanup(srv.Close)
	fails := make([]string, 0, 4)
	retryTesthook = func(i int, resp *http.Response, err error, d time.Duration) {
		code := 0
		if resp != nil {
			code = resp.StatusCode
		}
		fails = append(fails, fmt.Sprintf("%d %v %d %s", i, err, code, d))
	}
	t.Cleanup(func() { retryTesthook = nil })

	c := http.Client{
		Timeout: 1 * time.Second,
		Transport: Retry(http.DefaultTransport, 0, func(i int, resp *http.Response, err error) time.Duration {
			if err != nil {
				return -1
			}
			return time.Second
		}),
	}

	resp, err := c.Get(srv.URL)
	if err == nil || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("wrong error: %#v", err)
	}
	if resp != nil {
		t.Fatalf("resp is not nil: %v", resp)
	}
	want := []string{"0 <nil> 503 1s", "1 context deadline exceeded 0 -1ns"}
	if !reflect.DeepEqual(fails, want) {
		t.Errorf("\nhave: %#v\nwant: %#v", fails, want)
	}
}

func TestRetry_WithTimeout(t *testing.T) {
	var (
		i  int
		mu sync.Mutex
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if i < 4 {
			time.Sleep(2 * time.Second)
			w.WriteHeader(503)
		}
		w.Write([]byte("HANDLE"))
		i++
	}))
	t.Cleanup(srv.Close)
	fails := make([]string, 0, 4)
	retryTesthook = func(i int, resp *http.Response, err error, d time.Duration) {
		code := 0
		if resp != nil {
			code = resp.StatusCode
		}
		fails = append(fails, fmt.Sprintf("%d %v %d %s", i, err, code, d))
	}
	t.Cleanup(func() { retryTesthook = nil })

	c := http.Client{
		Transport: Retry(http.DefaultTransport, time.Millisecond*100, func(i int, resp *http.Response, err error) time.Duration {
			if i > 2 {
				return -1
			}
			return time.Second
		}),
	}

	start := time.Now()
	resp, err := c.Get(srv.URL)
	if err == nil || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("wrong error: %#v", err)
	}
	if resp != nil {
		t.Fatalf("resp is not nil: %v", resp)
	}
	want := []string{
		"0 context deadline exceeded 0 1s",
		"1 context deadline exceeded 0 1s",
		"2 context deadline exceeded 0 1s",
		"3 context deadline exceeded 0 -1ns",
	}
	if !reflect.DeepEqual(fails, want) {
		t.Errorf("\nhave: %#v\nwant: %#v", fails, want)
	}
	// Wait 3 seconds, + 3x~100ms for the request, + some overhead. Certainly
	// shouldn't wait the 2 seconds in the handler.
	if took := time.Since(start); took > 4*time.Second {
		t.Fatalf("took too long: %s", took)
	}
}
