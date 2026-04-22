package transport

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHTTPError(t *testing.T) {
	var i int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		code := 200
		switch i {
		case 0, 1:
			code = 400
		case 2, 3, 4, 5, 6:
			code = 500
		}
		w.WriteHeader(code)

		body := fmt.Sprintf("Handle request %d with code %d", i, code)
		if i == 4 {
			body = fmt.Sprintf(`{"key":"value","moar":"%s"}`, strings.Repeat("a", 8192))
		} else if i == 5 {
			body = fmt.Sprintf("{\n\t'key': 'value',\n\t'moar': '%s'\n}", strings.Repeat("a", 8192))
		} else if i == 6 {
			body = `{"key":"value"}` + "\n"
		}
		io.WriteString(w, body)
		i++
	}))
	t.Cleanup(srv.Close)

	tests := []struct {
		fn            func(resp *http.Response, err error) (*http.Response, error)
		want, wantErr string
	}{
		{HTTPError(true, 128), "400: Handle request 0 with code 400", ""},
		{HTTPError(false, 128), "", "HTTP status 400: Handle request 1 with code 400"},
		{HTTPError(true, 128), "", "HTTP status 500: Handle request 2 with code 500"},
		{HTTPError(false, 128), "", "HTTP status 500: Handle request 3 with code 500"},
		{HTTPError(false, 64), "", `HTTP status 500: {"key":"value","moar":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa…`},
		{HTTPError(false, 64), "", "HTTP status 500:\n{\n\t'key': 'value',\n\t'moar': 'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa…"},
		{HTTPError(false, 64), "", `HTTP status 500: {"key":"value"}`},
		{HTTPError(true, 128), "200: Handle request 7 with code 200", ""},
		{HTTPError(false, 128), "200: Handle request 8 with code 200", ""},
	}

	t.Parallel()
	for _, tt := range tests {
		t.Run("", func(t *testing.T) {
			c := &http.Client{
				Transport: Intercept(http.DefaultTransport, tt.fn),
			}

			resp, err := c.Get(srv.URL)
			if !errorContains(err, tt.wantErr) {
				t.Fatalf("wrong error\nhave: %v\nwant: %v", err, tt.wantErr)
			}
			if tt.wantErr != "" {
				return
			}
			defer resp.Body.Close()

			b, err := io.ReadAll(resp.Body)
			if err != nil {
				t.Fatal(err)
			}

			have := fmt.Sprintf("%d: %s", resp.StatusCode, b)
			if have != tt.want {
				t.Errorf("\nhave: %q\nwant: %q", have, tt.want)
			}
		})
	}
}
