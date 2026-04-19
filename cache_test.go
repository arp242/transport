package transport

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestCacheNop(t *testing.T) {
	t.Parallel()
	var i int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("HANDLE"))
		i++
	}))
	t.Cleanup(srv.Close)

	c := &http.Client{
		Transport: Cache(http.DefaultTransport, CacheNop(), nil),
	}

	for j := range 3 {
		b, err := mustGet(c, srv.URL)
		if err != nil {
			t.Fatalf("err on iter %d: %v", j, err)
		}
		if string(b) != "HANDLE" {
			t.Errorf("wrong body on iter %d: %q", j, string(b))
		}
		if i != j+1 {
			t.Errorf("i wrong on iter %d: %d", j, i)
		}
	}
}

func TestCacheMemory(t *testing.T) {
	t.Parallel()
	var i int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("HANDLE"))
		i++
	}))
	t.Cleanup(srv.Close)

	c := &http.Client{
		Transport: Cache(http.DefaultTransport, CacheMemory(), CacheExpireTime(time.Second)),
	}

	for j := range 3 {
		b, err := mustGet(c, srv.URL)
		if err != nil {
			t.Fatalf("err on iter %d: %v", j, err)
		}
		if string(b) != "HANDLE" {
			t.Errorf("wrong body on iter %d: %q", j, string(b))
		}
		if i != 1 {
			t.Errorf("i wrong on iter %d: %d", j, i)
		}
	}

	time.Sleep(time.Second)
	for j := range 3 {
		b, err := mustGet(c, srv.URL)
		if err != nil {
			t.Fatalf("err on iter %d: %v", j, err)
		}
		if string(b) != "HANDLE" {
			t.Errorf("wrong body on iter %d: %q", j, string(b))
		}
		if i != 2 {
			t.Errorf("i wrong on iter %d: %d", j, i)
		}
	}

	i = 0
	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { i++ }))
	t.Cleanup(srv2.Close)
	for j := range 3 {
		resp, err := c.Get(srv2.URL)
		if err != nil {
			t.Fatalf("err on iter %d: %v", j, err)
		}
		defer resp.Body.Close()

		if resp.Body != http.NoBody {
			t.Fatalf("resp.Body not http.NoBody on iter %d: %#v", j, resp.Body)
		}
		if i != 1 {
			t.Errorf("i wrong on iter %d: %d", j, i)
		}
	}
}

func TestCacheFile(t *testing.T) {
	t.Parallel()
	var i int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("HANDLE"))
		i++
	}))
	t.Cleanup(srv.Close)

	tmp := t.TempDir()
	c := &http.Client{
		Transport: Cache(http.DefaultTransport, CacheFile(tmp), CacheExpireTime(time.Second)),
	}

	for j := range 3 {
		b, err := mustGet(c, srv.URL)
		if err != nil {
			t.Fatalf("err on iter %d: %v", j, err)
		}
		if string(b) != "HANDLE" {
			t.Errorf("wrong body on iter %d: %q", j, string(b))
		}
		if i != 1 {
			t.Errorf("i wrong on iter %d: %d", j, i)
		}
	}

	time.Sleep(time.Second)
	for j := range 3 {
		b, err := mustGet(c, srv.URL)
		if err != nil {
			t.Fatalf("err on iter %d: %v", j, err)
		}
		if string(b) != "HANDLE" {
			t.Errorf("wrong body on iter %d: %q", j, string(b))
		}
		if i != 2 {
			t.Errorf("i wrong on iter %d: %d", j, i)
		}
	}

	i = 0
	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { i++ }))
	t.Cleanup(srv2.Close)
	for j := range 3 {
		resp, err := c.Get(srv2.URL)
		if err != nil {
			t.Fatalf("err on iter %d: %v", j, err)
		}
		defer resp.Body.Close()

		if resp.Body != http.NoBody {
			t.Fatalf("resp.Body not http.NoBody on iter %d: %#v", j, resp.Body)
		}
		if i != 1 {
			t.Errorf("i wrong on iter %d: %d", j, i)
		}
	}
}

func TestCacheAge(t *testing.T) {
	t.Parallel()
	var i int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("HANDLE"))
		i++
	}))
	t.Cleanup(srv.Close)

	c := &http.Client{
		Transport: Cache(http.DefaultTransport, CacheMemory(), CacheExpireTime(time.Hour)),
	}
	for j := range 3 {
		resp, err := c.Get(srv.URL)
		if err != nil {
			t.Fatalf("err on iter %d: %v", j, err)
		}
		defer resp.Body.Close()

		age := resp.Header.Get("Age")
		if j == 0 && age != "" {
			t.Error(age)
		} else if j == 1 && age != "2" {
			t.Error(age)
		} else if j == 2 && age != "3" {
			t.Error(age)
		}
		time.Sleep(time.Second)
	}
}
