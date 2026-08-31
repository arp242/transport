// Copy from https://github.com/arp242/zstd/tree/main/zio

package zio

import (
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
)

var (
	_ io.Reader = &peekReader{}
	_ io.Closer = &peekReader{}
	_ io.Closer = &testClose{}
)

type testClose struct {
	io.Reader
	didClose bool
}

func (tc *testClose) Close() error { tc.didClose = true; return nil }

func TestPeekReader(t *testing.T) {
	t.Run("read from both", func(t *testing.T) {
		r := PeekReader(strings.NewReader("hello"), []byte("abc"))
		buf := make([]byte, 10)
		n, err := r.Read(buf)
		if err != nil {
			t.Fatal(err)
		}
		if n != 8 {
			t.Error(n)
		}
		if h := string(buf); h != "abchello\x00\x00" {
			t.Errorf("%q", h)
		}

		buf = make([]byte, 10)
		n, err = r.Read(buf)
		if n != 0 {
			t.Error(n, string(buf))
		}
		if !errors.Is(err, io.EOF) {
			t.Fatal(err)
		}
	})

	t.Run("multiple reads from peeked", func(t *testing.T) {
		r := PeekReader(strings.NewReader("de"), []byte("abc"))
		for i := range 5 {
			buf := make([]byte, 1)
			n, err := r.Read(buf)
			if err != nil {
				t.Fatal(err)
			}
			if n != 1 {
				t.Error(n)
			}
			want := ""
			switch i {
			case 0:
				want = "a"
			case 1:
				want = "b"
			case 2:
				want = "c"
			case 3:
				want = "d"
			case 4:
				want = "e"
			}
			if h := string(buf); h != want {
				t.Error(h)
			}
		}

		buf := make([]byte, 10)
		n, err := r.Read(buf)
		if n != 0 {
			t.Error(n, string(buf))
		}
		if !errors.Is(err, io.EOF) {
			t.Fatal(err)
		}
	})

	t.Run("empty peeked", func(t *testing.T) {
		r := PeekReader(strings.NewReader("hello"), nil)
		buf := make([]byte, 10)
		n, err := r.Read(buf)
		if err != nil {
			t.Fatal(err)
		}
		if n != 5 {
			t.Error(n)
		}
		if h := string(buf[:n]); h != "hello" {
			t.Error(h)
		}
	})

	t.Run("close", func(t *testing.T) {
		tc := &testClose{Reader: strings.NewReader("hello")}
		r := PeekReader(tc, nil)
		err := r.Close()
		if err != nil {
			t.Fatal(err)
		}
		if !tc.didClose {
			t.Error("!tc.didClose")
		}
	})
}

func TestErrorReader(t *testing.T) {
	tests := []struct {
		in  string
		err error
	}{
		{"abc", errors.New("def")},
		{"abc", nil},
		{"abc", io.EOF},

		{strings.Repeat("abc", 20), errors.New("def")},
		{strings.Repeat("abc", 20), nil},
		{strings.Repeat("abc", 20), io.EOF},
	}

	for _, tt := range tests {
		t.Run("", func(t *testing.T) {
			r := ErrorReader(strings.NewReader(tt.in), tt.err)

			var (
				finalErr error
				data     []byte
				buf      = make([]byte, 8)
			)
			for {
				n, err := r.Read(buf)
				if n > 0 {
					data = append(data, buf[:n]...)
				}
				if err != nil {
					finalErr = err
					break
				}
			}
			if string(data) != tt.in {
				t.Fatalf("data after read not equal\ninput: %q\nafter read: %q", tt.in, string(data))
			}
			if tt.err == nil {
				tt.err = io.EOF
			}
			if finalErr != tt.err {
				t.Fatal(finalErr)
			}
		})
	}
}

func TestCopyReader(t *testing.T) {
	tests := []struct {
		in                 string
		limit              int64
		err                error
		wantFull, wantCopy string
	}{
		{"abc", 128, nil, `abc <nil>`, `abc <nil>`},
		{"A longer piece of text", 8, nil, `A longer piece of text <nil>`, `A longer <nil>`},
		{"abc", 3, nil, `abc <nil>`, `abc <nil>`},
		{"abc", 128, errors.New("def"), `abc def`, `«read error: def» <nil>`},
	}

	for _, tt := range tests {
		t.Run("", func(t *testing.T) {
			full, cp := CopyReader(io.NopCloser(ErrorReader(strings.NewReader(tt.in), tt.err)), tt.limit)

			dataFull, errFull := io.ReadAll(full)
			dataCopy, errCopy := io.ReadAll(cp)
			haveFull, haveCopy := fmt.Sprintf("%s %v", dataFull, errFull), fmt.Sprintf("%s %v", dataCopy, errCopy)

			if haveFull != tt.wantFull {
				t.Fatalf("full wrong\nhave: %s\nwant: %s", haveFull, tt.wantFull)
			}
			if haveCopy != tt.wantCopy {
				t.Fatalf("copy wrong\nhave: %s\nwant: %s", haveCopy, tt.wantCopy)
			}
		})
	}
}
