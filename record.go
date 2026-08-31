package transport

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptrace"

	"zgo.at/transport/internal/zio"
)

// Record a request and response.
func Record(parent http.RoundTripper, limitBody int64, recordRequest RecordRequest, recordResponse RecordResponse) *record {
	return &record{parent, limitBody, recordRequest, recordResponse}
}

// The RecordRequest and RecordResponse callbacks record the HTTP request and
// response.
//
// These functions do not return any errors: typically errors in Record() should
// not be fatal, and it's left to the caller to handle this (e.g. print to
// stderr, use a error log system, etc.)
//
// For every request, RecordRequest is called in one of two scenarios:
//
//  1. After a request is written, in which case all parameters except reqErr
//     are set. RecordResponse is called with the returned id after the request
//     finished. id should not be the zero value.
//
//     RecordResponse has either status, respHeader, and body set. Or it has
//     reqErr set. But not both.
//
//  2. If writing the request failed, in which case method, url, and reqErr
//     are set, but reqHeader and reqBody are not. RecordResponse is never
//     called and the return value doesn't matter.
//
// attrs is taken from the context, and can be set with [LogContext].
type (
	RecordRequest  func(ctx context.Context, method, url string, attrs []slog.Attr, reqHeader http.Header, reqBody io.Reader, reqErr error) (id string)
	RecordResponse func(ctx context.Context, id string, status int, respHeader http.Header, respBody io.Reader, roundtripErr error)
)

type record struct {
	parent         http.RoundTripper
	limit          int64
	recordRequest  RecordRequest
	recordResponse RecordResponse
}

func (t record) RoundTrip(r *http.Request) (*http.Response, error) {
	attrs, _ := getLogContext(r.Context())
	uri, m := r.RequestURI, "GET"
	if uri == "" {
		uri = r.URL.RequestURI()
	}
	if r.Method != "" {
		m = r.Method
	}

	var (
		id string
		b  io.Reader
		h  = make(http.Header)
	)
	r.Body, b = zio.CopyReader(r.Body, t.limit)
	r = r.WithContext(httptrace.WithClientTrace(r.Context(), &httptrace.ClientTrace{
		WroteHeaderField: func(k string, v []string) { h[k] = v },
		WroteRequest:     func(info httptrace.WroteRequestInfo) { id = t.recordRequest(r.Context(), m, uri, attrs, h, b, nil) },
	}))

	resp, rtErr := t.parent.RoundTrip(r)

	switch {
	case rtErr != nil && id == "": // e.g. we can't connect, so we never called WroteRequest.
		t.recordRequest(r.Context(), m, uri, attrs, nil, nil, rtErr)
	case rtErr != nil:
		t.recordResponse(r.Context(), id, 0, nil, nil, rtErr)
	default:
		var rb io.Reader
		resp.Body, rb = zio.CopyReader(resp.Body, t.limit)
		t.recordResponse(r.Context(), id, resp.StatusCode, resp.Header, rb, nil)
	}

	return resp, rtErr
}
