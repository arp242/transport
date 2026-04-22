package transport

import (
	"fmt"
	"io"
	"net/http"
	"slices"
)

// Intercept responses and change the response or error.
//
// One returned parameter must be nil: it's not allowed to return both a
// response and an error.
func Intercept(parent http.RoundTripper, fn func(*http.Response, error) (*http.Response, error)) *intercept {
	return &intercept{parent, fn}
}

type intercept struct {
	parent http.RoundTripper
	fn     func(*http.Response, error) (*http.Response, error)
}

func (t intercept) RoundTrip(r *http.Request) (*http.Response, error) {
	// http.Client enforces one value is nil, don't need to check that here.
	return t.fn(t.parent.RoundTrip(r))
}

// HTTPError returns an [Intercept] function to return errors if the HTTP status
// code is >=400 or >=500.
//
// This simplifies error handling for some common use cases.
//
// It only handles 5xx if only500 is set. Otherwise it will handle both 4xx and
// 5xx errors.
//
// It returns [ErrHTTPError], which contains the first bodyLimit bytes of the
// response body.
func HTTPError(only500 bool, bodyLimit int) func(resp *http.Response, err error) (*http.Response, error) {
	return func(resp *http.Response, err error) (*http.Response, error) {
		if err != nil || resp.StatusCode < 400 || (only500 && resp.StatusCode < 500) {
			return resp, err
		}

		//if err == nil && ((only500 && resp.StatusCode >= 500) || (!only500 && resp.StatusCode >= 400)) {
		hErr := ErrHTTPError{
			StatusCode:  resp.StatusCode,
			Status:      resp.Status,
			ContentType: resp.Header.Get("Content-Type"),
		}

		if bodyLimit > 0 {
			hErr.Body = make([]byte, bodyLimit)
			n, err := resp.Body.Read(hErr.Body)
			if err != nil && err != io.EOF {
				hErr.Body = append([]byte("error reading body: "), err.Error()...)
				n, err = len(hErr.Body), io.EOF
			}
			hErr.Body, hErr.FullBody = hErr.Body[:n], err == io.EOF
		}

		resp.Body.Close()
		return nil, hErr
	}
}

type ErrHTTPError struct {
	StatusCode  int
	Status      string
	ContentType string
	Body        []byte
	FullBody    bool
}

func (e ErrHTTPError) Error() string {
	nl := " "
	if slices.Contains(e.Body, '\n') && len(e.Body) > 0 && e.Body[len(e.Body)-1] != '\n' {
		nl = "\n"
	}
	more := ""
	if !e.FullBody {
		more = "…"
	}
	return fmt.Sprintf("HTTP status %d:%s%s%s", e.StatusCode, nl, e.Body, more)
}
