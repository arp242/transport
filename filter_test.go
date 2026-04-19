package transport

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestFilter(t *testing.T) {
	var i int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("HANDLE"))
		i++
	}))
	t.Cleanup(srv.Close)

	errTest := errors.New(`host "127.0.0.1" is not allowed`)
	c := &http.Client{
		Transport: Filter(http.DefaultTransport, func(u *url.URL) (bool, error) {
			switch i {
			case 1:
				i++
				return false, errTest
			case 2:
				i++
				return false, nil
			default:
				return true, nil
			}
		}),
	}

	// First is okay.
	b, err := mustGet(c, srv.URL)
	if err != nil || string(b) != "HANDLE" {
		t.Fatalf("err=%v; body=%s", err, string(b))
	}
	// Second errors with errTest.
	b, err = mustGet(c, srv.URL)
	if err == nil || !errors.Is(err, ErrFiltered) || !errors.Is(err, errTest) ||
		!strings.HasSuffix(err.Error(), `transport.Filter: request not allowed: host "127.0.0.1" is not allowed`) || b != "" {
		t.Fatalf("err=%v; body=%s", err, string(b))
	}
	// Third with nil.
	b, err = mustGet(c, srv.URL)
	if err == nil || !errors.Is(err, ErrFiltered) ||
		!strings.HasSuffix(err.Error(), `transport.Filter: request not allowed`) || b != "" {
		t.Fatalf("err=%v; body=%s", err, string(b))
	}
	// Fourth should be okay again.
	b, err = mustGet(c, srv.URL)
	if err != nil || string(b) != "HANDLE" {
		t.Fatalf("err=%v; body=%s", err, string(b))
	}
}

func TestFilterLocal(t *testing.T) {
	tests := []struct {
		in      string
		wantOk  bool
		wantErr string
	}{
		{"", false, `FilterLocal: empty scheme (invalid URL?)`},
		{"http://", false, `FilterLocal: empty host (invalid URL?)`},

		{"http://localhost", false, `FilterLocal: "localhost" is not allowed`},
		{"http://localhost:80", false, `FilterLocal: "localhost" is not allowed`},
		{"http://localhost:443", false, `FilterLocal: "localhost" is not allowed`},
		{"http://foo.localhost", false, `FilterLocal: "localhost" is not allowed`},

		{"http://127.0.0.1/foo", false, `FilterLocal: private IP addresses are not allowed`},
		{"http://10.1.2.3/foo", false, `FilterLocal: private IP addresses are not allowed`},
		{"http://10.1.2.3:80/foo", false, `FilterLocal: private IP addresses are not allowed`},
		{"http://0.0.0.0/foo", false, `FilterLocal: private IP addresses are not allowed`},
		{"http://[::1]/foo", false, `FilterLocal: private IP addresses are not allowed`},
		{"http://[::]/foo", false, `FilterLocal: private IP addresses are not allowed`},
	}

	for _, tt := range tests {
		t.Run("", func(t *testing.T) {
			u, err := url.Parse(tt.in)
			if err != nil {
				t.Fatal(err)
			}

			haveOk, haveErr := FilterLocal(u)
			if haveOk != tt.wantOk {
				t.Errorf("ok wrong\nhave: %v\nwant: %v", haveOk, tt.wantOk)
			}
			if !errorContains(haveErr, tt.wantErr) {
				t.Fatalf("wrong error\nhave: %v\nwant: %v", haveErr, tt.wantErr)
			}
		})
	}
}
