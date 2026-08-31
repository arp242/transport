// Package zio implements some I/O utility functions.
//
// Copy from https://github.com/arp242/zstd/tree/main/zio
package zio

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// PeekReader returns a reader that first returns data from peeked, before
// reading from the reader r.
//
// This is useful in cases where you want to "peek" a bit data from a reader
// that doesn't support seeking to determine if the compression or file format.
func PeekReader(r io.Reader, peeked []byte) io.ReadCloser {
	return &peekReader{r, peeked}
}

type peekReader struct {
	r      io.Reader
	peeked []byte
}

func (r *peekReader) Read(d []byte) (int, error) {
	if len(r.peeked) == 0 {
		return r.r.Read(d)
	}

	n := copy(d, r.peeked)
	r.peeked = r.peeked[n:]
	if len(r.peeked) > 0 {
		return n, nil
	}
	r.peeked = nil

	n2, err := r.r.Read(d[n:])
	return n + n2, err
}

// Close the underlying reader if it implements a Close method.
func (r *peekReader) Close() error {
	if c, ok := r.r.(io.Closer); ok {
		return c.Close()
	}
	return nil
}

// ErrorReader read all data from r and returns err on io.EOF.
func ErrorReader(r io.Reader, err error) io.Reader {
	// Passing nil here is a bit silly, but would get stuck in infinite loop
	// otherwise. So prevent that.
	if err == nil {
		err = io.EOF
	}
	return &errReader{r, err}
}

type errReader struct {
	r   io.Reader
	err error
}

func (r *errReader) Read(p []byte) (int, error) {
	n, err := r.r.Read(p)
	if err != nil && err != io.EOF {
		return n, err
	}
	if err == io.EOF {
		return 0, r.err
	}
	return n, err
}

// CopyReader copies up to limit bytes to a new reader. The "full" reader always
// reads the full data.
//
// The reader may be nil or [http.NoBody], in which case both return values are
// set to the same value.
//
// The copy is set to "«read error: %s»" and never returns any data if any error
// occurs. The full reader will always return any data read before the error (if
// any) and then returns the exact same error.
//
// This is intended to allow copying readers for debuggig or recording purposes,
// but not using a large amount of memory and not affecting the behaviour of the
// reader.
func CopyReader(r io.ReadCloser, limit int64) (full, cp io.ReadCloser) {
	if r == nil {
		return nil, nil
	}
	if r == http.NoBody {
		return http.NoBody, http.NoBody
	}

	d, err := io.ReadAll(io.LimitReader(r, limit))
	if err != nil {
		return io.NopCloser(ErrorReader(bytes.NewReader(d), err)),
			io.NopCloser(strings.NewReader(fmt.Sprintf(`«read error: %s»`, err)))
	}
	if limit > int64(len(d)) {
		return io.NopCloser(bytes.NewReader(d)), io.NopCloser(bytes.NewReader(d))
	}
	return PeekReader(r, d), io.NopCloser(bytes.NewReader(d))
}
