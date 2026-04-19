package transport_test

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"time"

	"zgo.at/transport"
)

func ExampleRetry() {
	var i int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if i < 1 {
			w.WriteHeader(500)
		}
		w.Write([]byte("Handle"))
		fmt.Printf("=> Handle attempt %d\n", i)
		i++
	}))
	defer srv.Close()

	c := http.Client{
		// The timeout applies to the entire request chain, including all
		// retries, so set this to an hour. The transport.Retry() function has a
		// timeout that applies to every individual retry attempt.
		Timeout: 1 * time.Hour,

		Transport: transport.Retry(http.DefaultTransport, 10*time.Second,
			func(i int, resp *http.Response, err error) time.Duration {
				if i > 10 { // Retry up to ten times.
					return -1
				}

				// Try to use Ratelimit headers first.
				d := transport.RetryRatelimit(0)(i, resp, err)
				if d == 0 {
					// Rate-Limit headers not present: use exponential backoff with
					// a maximum of 10 minutes.
					d = min(10*time.Minute, time.Duration(1<<i)*time.Second)
				}
				return d
			},
		),
	}

	resp, err := c.Get(srv.URL)
	if err != nil {
		log.Fatal(err)
	}

	b, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(string(b))

	// Output:
	// => Handle attempt 0
	// => Handle attempt 1
	// Handle
}

func ExampleCache() {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Println("=> Handle")
		w.Write([]byte("Response body"))
	}))
	defer srv.Close()

	c := http.Client{
		Transport: transport.Cache(
			http.DefaultTransport,
			transport.CacheMemory(),              // Cache in memory.
			transport.CacheExpireTime(time.Hour), // Expire after an hour.
		),
	}

	read := func(resp *http.Response) string {
		defer resp.Body.Close()
		b, err := io.ReadAll(resp.Body)
		if err != nil {
			log.Fatal(err)
		}
		return string(b)
	}
	resp, err := c.Get(srv.URL)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(read(resp))

	// Second request: same response body but handler not called.
	resp, err = c.Get(srv.URL)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(read(resp))

	// Output:
	// => Handle
	// Response body
	// Response body
}

func ExampleFilter() {
	c := http.Client{
		// Disallow all requests to local/private addresses such as localhost, 10/8, etc.
		Transport: transport.Filter(http.DefaultTransport, transport.FilterLocal),
	}
	resp, err := c.Get("http://localhost/test")
	fmt.Println(err)
	fmt.Println(resp == nil)

	// Output:
	// Get "http://localhost/test": transport.Filter: request not allowed: FilterLocal: "localhost" is not allowed
	// true
}

func ExampleLog() {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("Handle"))
	}))
	defer srv.Close()

	buf := new(bytes.Buffer)
	c := http.Client{
		// Log request and response headers and body.
		Transport: transport.Log(http.DefaultTransport, buf, transport.LogAll),
	}
	resp, err := c.Post(srv.URL, "application/json", strings.NewReader(`[1, 2, 3]`))
	if err != nil {
		log.Fatal(err)
	}
	resp.Body.Close()

	// Replace some dynamic text with static text for test.
	have := buf.String()
	have = regexp.MustCompile(`(Host: *127\.0\.0\.1:)\d+`).ReplaceAllString(have, `$1`)
	have = regexp.MustCompile(`(Date: *).+?\n`).ReplaceAllString(have, "${1}Tue, 21 Apr 2026 21:13:48 GMT\n")
	fmt.Print(have)

	// Output:
	// REQ │ POST / HTTP/1.1
	// REQ │ Host:            127.0.0.1:
	// REQ │ Accept-Encoding: gzip
	// REQ │ Content-Length:  9
	// REQ │ Content-Type:    application/json
	// REQ │ User-Agent:      Go-http-client/1.1
	// REQ │
	// REQ │ [1, 2, 3]
	//     ├────────────────────────────────────────────────────────────
	// RES │ Content-Length: 6
	// RES │ Content-Type:   text/plain; charset=utf-8
	// RES │ Date:           Tue, 21 Apr 2026 21:13:48 GMT
	// RES │
	// RES │ Handle
}
