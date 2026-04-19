package transport

import (
	"errors"
	"fmt"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
)

// Filter requests.
//
// Requests return [ErrFiltered] if allow returns false. It can optionally
// return an error with some additional information.
func Filter(parent http.RoundTripper, allow func(*url.URL) (bool, error)) *filter {
	return &filter{parent, allow}
}

type filter struct {
	parent http.RoundTripper
	allow  func(*url.URL) (bool, error)
}

var ErrFiltered = errors.New("transport.Filter: request not allowed")

func (t filter) RoundTrip(r *http.Request) (*http.Response, error) {
	if ok, err := t.allow(r.URL); !ok {
		if err != nil {
			return nil, fmt.Errorf("%w: %w", ErrFiltered, err)
		}
		return nil, ErrFiltered
	}
	return t.parent.RoundTrip(r)
}

// FilterLocal filters all requests to local addresses.
func FilterLocal(u *url.URL) (bool, error) {
	if u.Scheme == "" {
		return false, errors.New("FilterLocal: empty scheme (invalid URL?)")
	}
	if u.Host == "" {
		return false, errors.New("FilterLocal: empty host (invalid URL?)")
	}

	p := u.Port()
	if p != "" && p != "80" && p != "443" {
		return false, errors.New("FilterLocal: only port 80 and 443 are allowed")
	}

	h := u.Host
	if p != "" {
		h = h[:len(h)-len(p)-1]
	}
	if h == "localhost" || strings.HasSuffix(h, ".localhost") {
		return false, errors.New(`FilterLocal: "localhost" is not allowed`)
	}

	ip, err := netip.ParseAddr(strings.Trim(h, "[]"))
	if err == nil {
		if ip.IsPrivate() || !ip.IsGlobalUnicast() {
			return false, errors.New(`FilterLocal: private IP addresses are not allowed`)
		}
	}
	return true, nil
}
