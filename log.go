package transport

import (
	"bytes"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/http/httptrace"
	"os"
	"slices"
	"strconv"
	"strings"
)

type LogOption uint64

const (
	LogAll            = LogRequestHeaders | LogRequestBody | LogResponseHeaders | LogResponseBody
	LogRequestHeaders = LogOption(1 << iota)
	LogRequestBody
	LogResponseHeaders
	LogResponseBody
)

func (l LogOption) has(ll LogOption) bool { return l&ll != 0 }

// Log writes request and/or response details to out.
func Log(parent http.RoundTripper, out io.Writer, what LogOption) *log {
	bold, reset := "", ""
	if os.Getenv("NO_COLOR") == "" {
		if fp, ok := out.(*os.File); ok && (fp.Fd() == 1 || fp.Fd() == 2) {
			bold, reset = "\x1b[1m", "\x1b[0m"
		}
	}
	return &log{parent, out, what, bold, reset}
}

type log struct {
	parent      http.RoundTripper
	out         io.Writer
	what        LogOption
	bold, reset string
}

func (t log) RoundTrip(r *http.Request) (*http.Response, error) {
	if t.what != 0 {
		reqURI, method := r.RequestURI, "GET"
		if reqURI == "" {
			reqURI = r.URL.RequestURI()
		}
		if r.Method != "" {
			method = r.Method
		}

		fmt.Fprintf(t.out, "%sREQ │ %s %s HTTP/%d.%d%s\n", t.bold, method, reqURI, r.ProtoMajor, r.ProtoMinor, t.reset)
	}

	// Log request headers via trace, so we can log exactly what's being sent.
	// Logging r.Header won't do that as headers are added by the http package.
	var trace *httptrace.ClientTrace
	if t.what.has(LogRequestHeaders) {
		h := make(http.Header)
		trace = &httptrace.ClientTrace{
			WroteHeaderField: func(k string, v []string) { h[k] = v },
			WroteHeaders:     func() { printHeaders(t.out, "REQ │ ", h) },
		}
	}
	if t.what.has(LogRequestBody) {
		var b []byte
		if r.Body != nil && r.Body != http.NoBody {
			b, _ = io.ReadAll(r.Body)
			r.Body = io.NopCloser(bytes.NewReader(b))
		}
		if trace == nil {
			trace = &httptrace.ClientTrace{}
		}
		trace.WroteRequest = func(info httptrace.WroteRequestInfo) {
			if t.what.has(LogRequestHeaders) {
				fmt.Fprintln(t.out, "REQ │")
			}
			printBody(t.out, "REQ │ ", b)
		}
	}
	if trace != nil {
		r = r.WithContext(httptrace.WithClientTrace(r.Context(), trace))
	}

	resp, err := t.parent.RoundTrip(r)

	if (t.what.has(LogRequestHeaders) || t.what.has(LogRequestBody)) &&
		(t.what.has(LogResponseHeaders) || t.what.has(LogResponseBody)) {
		fmt.Fprint(t.out, "    ├"+strings.Repeat("─", 60)+"\n")
	}

	if resp != nil && t.what.has(LogResponseHeaders) {
		printHeaders(t.out, "RES │ ", resp.Header)
	}
	if t.what.has(LogResponseBody) {
		if t.what.has(LogResponseHeaders) {
			fmt.Fprintln(t.out, "RES │")
		}
		var b []byte
		if resp.Body != nil && resp.Body != http.NoBody {
			b, _ = io.ReadAll(resp.Body)
			resp.Body = io.NopCloser(bytes.NewReader(b))
		}
		printBody(t.out, "RES │ ", b)
	}

	return resp, err
}

func printHeaders(out io.Writer, prefix string, header http.Header) {
	keys, l := make([]string, 0, len(header)), 0
	for k := range maps.Keys(header) {
		keys, l = append(keys, k), max(l, len(k))
	}
	slices.Sort(keys)
	f := prefix + "%-" + strconv.Itoa(l+1) + "s %v\n"
	if h := header.Get("Host"); h != "" { // Always print Host: first.
		fmt.Fprintf(out, f, "Host:", h)
	}
	for _, k := range keys {
		if k == "Host" {
			continue
		}
		for _, v := range header[k] {
			fmt.Fprintf(out, f, k+":", v)
		}
	}
}

// TODO: instead of reading the entire body, we can wrap it in a reader that
// prints as it's read. I think that should work?
func printBody(out io.Writer, prefix string, b []byte) {
	if b == nil {
		fmt.Fprint(out, prefix)
		fmt.Fprintln(out, "«http.NoBody»")
		return
	}

	// Binary data as hexdump-like output.
	// TODO: this can/should look at Content-Type header. Can/should also print
	// things like JSON nicer.
	if slices.Contains(b, 0) {
		var (
			left1  = make([]byte, 0, 23)
			left2  = make([]byte, 0, 23)
			right1 = make([]byte, 0, 8)
			right2 = make([]byte, 0, 8)
			dir    bool
		)
		for i, c := range b {
			if i > 0 && i%16 == 0 {
				fmt.Fprintf(out, "%s%- 23x │ %- 23x   |%-8s|%-8s|\n", prefix, left1, left2, right1, right2)
				left1, left2, right1, right2 = left1[:0], left2[:0], right1[:0], right2[:0]
			}

			if i%8 == 0 {
				dir = !dir
			}
			if dir {
				left1 = append(left1, c)
			} else {
				left2 = append(left2, c)
			}
			if c < 0x20 || c == 0x7f {
				c = '.'
			}
			if dir {
				right1 = append(right1, c)
			} else {
				right2 = append(right2, c)
			}
		}
		fmt.Fprintf(out, "%s%- 23x │ %- 23x   |%-8s|%-8s|\n", prefix, left1, left2, right1, right2)
		return
	}

	// Plain text.
	fmt.Fprint(out, prefix)
	line := make([]byte, 0, 256)
	for _, c := range b {
		if c == '\n' {
			fmt.Fprintln(out, string(line))
			fmt.Fprint(out, prefix)
			line = line[:0]
			continue
		}
		if len(line) == 8192 {
			fmt.Fprint(out, string(line))
			line = line[:0]
		}
		line = append(line, c)
	}
	fmt.Fprintln(out, string(line))
}
