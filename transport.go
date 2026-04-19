// Package transport contains HTTP transports for http.Client.
//
// All of these implement [http.RoundTripper] so it can be used with any
// [http.Client] and won't require any changes other than setting the
// [http.Client.Transport] field.
//
// Every transport accepts a parent transport; multiple transports can be used
// by calling several of them. For example:
//
//	c := http.Client{
//		Transport: transport.Retry(transport.Cache(http.DefaultTransport)),
//	}
//
// This is run from the outer-most call to the inner-most (Retry → Cache →
// DefaultTransport).
package transport
