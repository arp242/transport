package transport

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"
)

type (
	// CacheStorer is a cache storer.
	CacheStorer interface {
		// Get a stored cache entry. The error return is the cached error (if
		// any), not an error return for this function.
		Get(*http.Request) (*http.Response, error, bool)

		// Put stores a new cache Response and error for the request.
		Put(*http.Request, *http.Response, error)
	}

	// CacheExpirer determines if a cached response is expired.
	CacheExpirer interface {
		Expired(s CacheStorer, cached *http.Response, cachedErr error) bool
	}
)

var (
	_ CacheStorer  = (*cacheNop)(nil)
	_ CacheStorer  = (*cacheMemory)(nil)
	_ CacheStorer  = (*cacheFile)(nil)
	_ CacheExpirer = (*expireTime)(nil)
)

type cache struct {
	parent http.RoundTripper
	store  CacheStorer
	expire CacheExpirer
}

func (t cache) RoundTrip(r *http.Request) (*http.Response, error) {
	if resp, err, ok := t.store.Get(r); ok {
		// Cache expired: we don't delete it, but it is overwritten later.
		if t.expire == nil || !t.expire.Expired(t.store, resp, err) {
			age, _ := strconv.Atoi(resp.Header.Get("Age"))
			resp.Header.Set("Age", strconv.Itoa(max(age, int(math.Ceil(time.Since(getDate(resp)).Seconds())))))
			return resp, err
		}
	}

	resp, err := t.parent.RoundTrip(r)

	if resp != nil && resp.Header.Get("Date") == "" { // Just in case it's not set.
		resp.Header.Set("Date", time.Now().Format(time.RFC1123))
	}
	t.store.Put(r, resp, err)

	return resp, err
}

// Cache requests in the given storer.
//
// The expiry is determined by the expirer, which may be nil to cache forever.
// Expired resourced are not cleaned: cached resources can only be overwritten
// with a newer version.
func Cache(parent http.RoundTripper, s CacheStorer, e CacheExpirer) *cache {
	return &cache{parent, s, e}
}

// CacheNop returns a storer that doesn't do anything.
func CacheNop() *cacheNop { return &cacheNop{} }

type cacheNop struct{}

func (s *cacheNop) Get(*http.Request) (*http.Response, error, bool) { return nil, nil, false }
func (s *cacheNop) Put(*http.Request, *http.Response, error)        {}

// CacheMemory caches request in memory.
//
// Because expired resourced are not cleaned this may use a large amount of
// memory over time. This implementation is intentionally kept simple – use a
// more advance memory cache such as e.g. https://zgo.at/zcache if you need to
// clean expired resources.
func CacheMemory() *cacheMemory {
	return &cacheMemory{
		cache: make(map[string]struct {
			r *http.Response
			e error
			b []byte
		}),
	}
}

type cacheMemory struct {
	mu    sync.RWMutex
	cache map[string]struct {
		r *http.Response
		e error
		b []byte
	}
}

func (s *cacheMemory) Get(r *http.Request) (*http.Response, error, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	c, ok := s.cache[r.URL.String()]
	if c.r != nil {
		if c.b == nil {
			c.r.Body = http.NoBody
		} else {
			c.r.Body = io.NopCloser(bytes.NewReader(c.b))
		}
	}
	return c.r, c.e, ok
}

func (s *cacheMemory) Put(r *http.Request, resp *http.Response, respErr error) {
	var b []byte
	if resp != nil && resp.Body != nil && resp.Body != http.NoBody {
		var err error
		b, err = io.ReadAll(resp.Body)
		resp.Body = io.NopCloser(bytes.NewReader(b))
		if err != nil {
			_ = err // XXX: do something with this error?
			return
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.cache[r.URL.String()] = struct {
		r *http.Response
		e error
		b []byte
	}{resp, respErr, b}
}

// CacheFile caches resources on disk.
func CacheFile(path string) *cacheFile {
	return &cacheFile{path: path}
}

type cacheFile struct {
	mu   sync.RWMutex
	path string
}

func (s *cacheFile) Get(r *http.Request) (*http.Response, error, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	fp, err := os.Open(s.cachepath(r))
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			_ = err // XXX: do something with this error?
		}
		return nil, nil, false
	}

	var cached struct {
		http.Response
		Body  []byte
		Error error
	}
	err = json.NewDecoder(fp).Decode(&cached)
	if err != nil {
		_ = err // XXX: do something with this error?
		return nil, nil, false
	}

	resp := &cached.Response
	if cached.Body == nil {
		resp.Body = http.NoBody
	} else {
		resp.Body = io.NopCloser(bytes.NewReader(cached.Body))
	}
	resp.Request = r

	return resp, cached.Error, true
}

func (s *cacheFile) Put(r *http.Request, resp *http.Response, respErr error) {
	var b []byte
	if resp != nil && resp.Body != nil && resp.Body != http.NoBody {
		var err error
		b, err = io.ReadAll(resp.Body)
		resp.Body = io.NopCloser(bytes.NewReader(b))
		if err != nil {
			_ = err // XXX: do something with this error?
			return
		}
	}

	//lint:ignore SA1026 https://github.com/dominikh/go-tools/issues/1712
	j, err := json.Marshal(struct {
		*http.Response
		Body    []byte
		Error   error
		URL     string
		Request *http.Request
	}{Response: resp, Error: respErr, URL: r.URL.String(), Body: b})
	if err != nil {
		_ = err // XXX: do something with this error?
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	err = os.WriteFile(s.cachepath(r), j, 0o777)
	_ = err // XXX: do something with this error?
}

func (s *cacheFile) cachepath(r *http.Request) string {
	h := sha256.New()
	h.Write([]byte(r.URL.String()))
	return filepath.Join(s.path, hex.EncodeToString(h.Sum(nil))) + ".json"
}

// CacheExpireTime expires resources if they're older than d.
//
// This does not look at any HTTP cache headers; it simply cached for the
// duration of d.
func CacheExpireTime(d time.Duration) *expireTime { return &expireTime{d} }

type expireTime struct{ d time.Duration }

func (e expireTime) Expired(s CacheStorer, cached *http.Response, err error) bool {
	return time.Since(getDate(cached)) > e.d
}

type cachedTime struct{}

// Cache on the context to avoid re-parsing the Date all the time. Doing it on
// the request context means we won't have to worry about managing/expiring a
// separate time parse cache.
func getDate(resp *http.Response) time.Time {
	t, ok := resp.Request.Context().Value(cachedTime{}).(time.Time)
	if !ok {
		t, _ = time.Parse(time.RFC1123, resp.Header.Get("Date"))
		*resp.Request = *resp.Request.WithContext(context.WithValue(resp.Request.Context(), cachedTime{}, t))
	}
	return t
}
