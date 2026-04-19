package transport

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/synctest"
)

func TestLog(t *testing.T) {
	tests := []struct {
		what    LogOption
		req     string
		reqBody string
		want    string
	}{
		{0, "GET %s", `hello`, ``},

		// Empty request body.
		{LogRequestBody, "GET %s", ``, `
			REQ │ GET / HTTP/1.1
			REQ │ «http.NoBody»
		`[1:]},
		{LogRequestBody, "GET %s", `http.NoBody`, `
			REQ │ GET / HTTP/1.1
			REQ │ «http.NoBody»
		`[1:]},
		{LogRequestBody, "GET %s", `nil`, `
			REQ │ GET / HTTP/1.1
			REQ │ «http.NoBody»
		`[1:]},
		// Text request body.
		{LogRequestBody, "GET %s", `hello`, `
			REQ │ GET / HTTP/1.1
			REQ │ hello
		`[1:]},
		{LogRequestBody, "GET %s", "hello\n", `
			REQ │ GET / HTTP/1.1
			REQ │ hello
			REQ │·
		`[1:]},
		{LogRequestBody, "GET %s", "hello\n world", `
			REQ │ GET / HTTP/1.1
			REQ │ hello
			REQ │  world
		`[1:]},
		{LogRequestBody, "GET %s", "hello\n world\n", `
			REQ │ GET / HTTP/1.1
			REQ │ hello
			REQ │  world
			REQ │·
		`[1:]},
		// Binary request body.
		{LogRequestBody, "GET %s", "b\x00d\x00", `
			REQ │ GET / HTTP/1.1
			REQ │ 62 00 64 00             │                           |b.d.    |        |
		`[1:]},
		{LogRequestBody, "GET %s", "binary\x00data\x00", `
			REQ │ GET / HTTP/1.1
			REQ │ 62 69 6e 61 72 79 00 64 │ 61 74 61 00               |binary.d|ata.    |
		`[1:]},
		{LogRequestBody, "GET %s", "binary\x00data\x00\nmoar moar moar\n", `
			REQ │ GET / HTTP/1.1
			REQ │ 62 69 6e 61 72 79 00 64 │ 61 74 61 00 0a 6d 6f 61   |binary.d|ata..moa|
			REQ │ 72 20 6d 6f 61 72 20 6d │ 6f 61 72 0a               |r moar m|oar.    |
		`[1:]},
		{LogRequestBody, "GET %s", "binary\x00data\x00\nmoar moar moarmoar\n", `
			REQ │ GET / HTTP/1.1
			REQ │ 62 69 6e 61 72 79 00 64 │ 61 74 61 00 0a 6d 6f 61   |binary.d|ata..moa|
			REQ │ 72 20 6d 6f 61 72 20 6d │ 6f 61 72 6d 6f 61 72 0a   |r moar m|oarmoar.|
		`[1:]},
		// Request Headers.
		{LogRequestHeaders, "GET %s", `hello`, `
			REQ │ GET / HTTP/1.1
			REQ │ Host:            %HOST%
			REQ │ Accept-Encoding: gzip
			REQ │ Content-Length:  5
			REQ │ User-Agent:      Go-http-client/1.1
		`[1:]},
		{LogRequestHeaders | LogRequestBody, "GET %s", ``, `
			REQ │ GET / HTTP/1.1
			REQ │ Host:            %HOST%
			REQ │ Accept-Encoding: gzip
			REQ │ User-Agent:      Go-http-client/1.1
			REQ │
			REQ │ «http.NoBody»
		`[1:]},
		{LogRequestHeaders | LogRequestBody, "GET %s", `hello`, `
			REQ │ GET / HTTP/1.1
			REQ │ Host:            %HOST%
			REQ │ Accept-Encoding: gzip
			REQ │ Content-Length:  5
			REQ │ User-Agent:      Go-http-client/1.1
			REQ │
			REQ │ hello
		`[1:]},
		{LogRequestHeaders | LogRequestBody, "GET %s", "hello\nworld\n", `
			REQ │ GET / HTTP/1.1
			REQ │ Host:            %HOST%
			REQ │ Accept-Encoding: gzip
			REQ │ Content-Length:  12
			REQ │ User-Agent:      Go-http-client/1.1
			REQ │
			REQ │ hello
			REQ │ world
			REQ │·
		`[1:]},
		{LogRequestHeaders | LogRequestBody, "GET %s", "binary\x00data", `
			REQ │ GET / HTTP/1.1
			REQ │ Host:            %HOST%
			REQ │ Accept-Encoding: gzip
			REQ │ Content-Length:  11
			REQ │ User-Agent:      Go-http-client/1.1
			REQ │
			REQ │ 62 69 6e 61 72 79 00 64 │ 61 74 61                  |binary.d|ata     |
		`[1:]},

		// Response.
		{LogResponseHeaders, "GET %s", "", `
			REQ │ GET / HTTP/1.1
			RES │ Content-Length: 6
			RES │ Content-Type:   text/plain; charset=utf-8
			RES │ Date:           Sat, 01 Jan 2000 00:00:00 GMT
		`[1:]},
		{LogResponseBody, "GET %s", "", `
			REQ │ GET / HTTP/1.1
			RES │ HANDLE
		`[1:]},
		{LogResponseHeaders | LogResponseBody, "GET %s", "", `
			REQ │ GET / HTTP/1.1
			RES │ Content-Length: 6
			RES │ Content-Type:   text/plain; charset=utf-8
			RES │ Date:           Sat, 01 Jan 2000 00:00:00 GMT
			RES │
			RES │ HANDLE
		`[1:]},
		{LogRequestHeaders | LogResponseHeaders | LogResponseBody, "GET %s", "", `
			REQ │ GET / HTTP/1.1
			REQ │ Host:            %HOST%
			REQ │ Accept-Encoding: gzip
			REQ │ User-Agent:      Go-http-client/1.1
			    ├────────────────────────────────────────────────────────────
			RES │ Content-Length: 6
			RES │ Content-Type:   text/plain; charset=utf-8
			RES │ Date:           Sat, 01 Jan 2000 00:00:00 GMT
			RES │
			RES │ HANDLE
		`[1:]},
		{LogRequestHeaders | LogRequestBody | LogResponseHeaders | LogResponseBody, "GET %s", "", `
			REQ │ GET / HTTP/1.1
			REQ │ Host:            %HOST%
			REQ │ Accept-Encoding: gzip
			REQ │ User-Agent:      Go-http-client/1.1
			REQ │
			REQ │ «http.NoBody»
			    ├────────────────────────────────────────────────────────────
			RES │ Content-Length: 6
			RES │ Content-Type:   text/plain; charset=utf-8
			RES │ Date:           Sat, 01 Jan 2000 00:00:00 GMT
			RES │
			RES │ HANDLE
		`[1:]},
	}

	for _, tt := range tests {
		t.Run("", func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) { // synctest for consistent time.
				var i int
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.Write([]byte("HANDLE"))
					i++
				}))
				defer srv.Close()

				verb, u, _ := strings.Cut(tt.req, " ")
				var rb io.Reader = strings.NewReader(tt.reqBody)
				if tt.reqBody == "http.NoBody" {
					rb = http.NoBody
				} else if tt.reqBody == "nil" {
					rb = nil
				}
				r, err := http.NewRequest(verb, fmt.Sprintf(u, srv.URL), rb)
				if err != nil {
					t.Fatal(err)
				}

				have := new(bytes.Buffer)
				c := &http.Client{
					Transport: Log(http.DefaultTransport, have, tt.what),
				}
				resp, err := c.Do(r)
				if err != nil {
					t.Fatal(err)
				}
				defer resp.Body.Close()
				b, err := io.ReadAll(resp.Body)
				if err != nil {
					t.Fatal(err)
				}
				if string(b) != "HANDLE" {
					t.Fatalf("body=%s", string(b))
				}

				tt.want = strings.ReplaceAll(tt.want, "\t", "")
				tt.want = strings.ReplaceAll(tt.want, "·\n", " \n")
				tt.want = strings.ReplaceAll(tt.want, "%HOST%", srv.Listener.Addr().String())
				h := have.String()
				if h != tt.want {
					t.Errorf("\nhave:\n%s\nwant:\n%s", h, tt.want)
					//t.Logf("have: %q", h)
					//t.Logf("want: %q", tt.want)
				}
			})
		})
	}
}
