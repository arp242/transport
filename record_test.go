package transport

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"

	"strings"

	"testing"
)

func TestRecord(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("server text"))
	}))
	defer srv.Close()

	ctx := LogContext(t.Context(), "key", "val", "k2", 123)
	ctxCancel, cancel := context.WithCancel(ctx)

	tests := []struct {
		ctx               context.Context
		host              string
		wantBody          []byte
		clientErr         string
		wantReq, wantResp map[string]any
	}{
		// Succeeded in making the request, but some error while reading response.
		// Needs to be first as context is cancelled on first loop.
		{ctxCancel, srv.Listener.Addr().String(), nil, "context canceled",
			map[string]any{
				"body":    "body text",
				"bodyErr": nil,
				"err":     nil,
				"method":  "POST",
				"url":     "/",
				"attr":    []slog.Attr{slog.String("key", "val"), slog.Int("k2", 123)},
				"headers": http.Header{
					"Accept-Encoding": []string{"gzip"},
					"Content-Length":  []string{"9"},
					"Host":            []string{"127.0.0.1:39701"},
					"User-Agent":      []string{"Go-http-client/1.1"},
				},
			},
			map[string]any{
				"status":  0,
				"err":     "context canceled",
				"id":      "AAA",
				"body":    "",
				"bodyErr": nil,
				"headers": http.Header(nil),
			},
		},

		// Normal path
		{ctx, srv.Listener.Addr().String(), []byte("server text"), "",
			map[string]any{
				"body":    "body text",
				"bodyErr": nil,
				"err":     nil,
				"method":  "POST",
				"url":     "/",
				"attr":    []slog.Attr{slog.String("key", "val"), slog.Int("k2", 123)},
				"headers": http.Header{
					"Accept-Encoding": []string{"gzip"},
					"Content-Length":  []string{"9"},
					"Host":            []string{"127.0.0.1:39701"},
					"User-Agent":      []string{"Go-http-client/1.1"},
				},
			},
			map[string]any{
				"status":  200,
				"err":     nil,
				"id":      "AAA",
				"body":    "server text",
				"bodyErr": nil,
				"headers": http.Header{
					"Content-Length": []string{"11"},
					"Content-Type":   []string{"text/plain; charset=utf-8"},
					"Date":           []string{"Sat, 01 Jan 2000 00:00:00 GMT"},
				},
			},
		},

		// Connect error
		{ctx, "127.0.0.99:123", nil, "dial tcp 127.0.0.99:123: connect: connection refused",
			map[string]any{
				"body":    "",
				"bodyErr": nil,
				"err":     "dial tcp 127.0.0.99:123: connect: connection refused",
				"method":  "POST",
				"url":     "/",
				"attr":    []slog.Attr{slog.String("key", "val"), slog.Int("k2", 123)},
				"headers": http.Header(nil),
			},
			(map[string]any)(nil),
		},
	}

	for _, tt := range tests {
		t.Run("", func(t *testing.T) {
			var haveReq, haveResp map[string]any
			c := &http.Client{
				Transport: Record(http.DefaultTransport, 16,
					func(ctx context.Context, method, url string, attr []slog.Attr, reqHeader http.Header, reqBody io.Reader, reqErr error) (id string) {
						var (
							b   []byte
							err error
						)
						if reqBody != nil {
							b, err = io.ReadAll(reqBody)
						}
						haveReq = map[string]any{
							"method":  method,
							"url":     url,
							"headers": reqHeader,
							"body":    string(b),
							"bodyErr": err,
							"err":     reqErr,
							"attr":    attr,
						}
						if reqErr != nil {
							haveReq["err"] = reqErr.Error()
						}
						cancel()
						return "AAA"
					},
					func(ctx context.Context, id string, status int, respHeader http.Header, respBody io.Reader, roundtripErr error) {
						var (
							b   []byte
							err error
						)
						if respBody != nil {
							b, err = io.ReadAll(respBody)
						}
						haveResp = map[string]any{
							"id":      id,
							"status":  status,
							"headers": respHeader,
							"err":     roundtripErr,
							"body":    string(b),
							"bodyErr": err,
						}
						if roundtripErr != nil {
							haveResp["err"] = roundtripErr.Error()
						}
					},
				),
			}

			r, _ := http.NewRequest("POST", "http://"+tt.host, strings.NewReader("body text"))
			*r = *r.WithContext(tt.ctx)

			resp, err := c.Do(r)
			if !errorContains(err, tt.clientErr) {
				t.Fatal(err)
			}

			var haveBody []byte
			if resp != nil {
				haveBody, err = io.ReadAll(resp.Body)
				if err != nil {
					t.Fatal(err)
				}
				resp.Body.Close()
			}

			if !slices.Equal(haveBody, tt.wantBody) {
				t.Fatal(haveBody)
			}

			if h, ok := tt.wantReq["headers"].(http.Header); ok && len(h) > 0 {
				h.Set("Host", tt.host)
			}
			if h, ok := tt.wantResp["headers"].(http.Header); ok && len(h) > 0 {
				if hh, ok := haveResp["headers"].(http.Header); ok {
					h.Set("Date", hh.Get("Date"))
				}
			}

			if !reflect.DeepEqual(haveReq, tt.wantReq) {
				t.Fatalf("request wrong\nhave: %#v\nwant: %#v", haveReq, tt.wantReq)
			}
			if !reflect.DeepEqual(haveResp, tt.wantResp) {
				t.Fatalf("response wrong\nhave: %#v\nwant: %#v", haveResp, tt.wantResp)
			}
		})
	}
}
