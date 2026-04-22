HTTP transports for Go's http.Client.

All of these implement http.RoundTripper so it can be used with any http.Client
and won't require any changes other than setting the Client.Transport field.

Included transports:

- Retry – retry on errors and ratelimits.

- Cache – cache responses in memory or on disk.

- Filter – filter some requests, such as local/private addresses.

- Intercept – intercept responses and change the response or error, useful to e.g. automatically return errors on 5xx responses.

- Log – print logs for debugging.

Every transport accepts a parent transport; multiple transports can be used by
calling several of them. For example:

```go
c := http.Client{
    Transport: transport.Retry(transport.Cache(http.DefaultTransport)),
}
```

This is run from the outer-most call to the inner-most (Retry → Cache →
DefaultTransport).

Retry
-----

```go
c := http.Client{
    // The timeout applies to the entire request chain, including all
    // retries, so set this to an hour. The transport.Retry() function has a
    // timeout that applies to every individual retry attempt.
    Timeout: 1 * time.Hour,

    Transport: transport.Retry(http.DefaultTransport, 10*time.Second,
        func(i int, resp *http.Response, err error) time.Duration {
            if i > 10 { // Retry up to ten times.
                return -1
            }

            // Try to use Ratelimit headers first.
            d := transport.RetryRatelimit(0)(i, resp, err)
            if d == 0 {
                // Rate-Limit headers not present: use exponential backoff with
                // a maximum of 10 minutes.
                d = min(10*time.Minute, time.Duration(1<<i)*time.Second)
            }
            return d
        },
    ),
}
```

Cache
-----

```go
c := http.Client{
    Transport: transport.Cache(
        http.DefaultTransport,
        transport.CacheMemory(),              // Cache in memory.
        transport.CacheExpireTime(time.Hour), // Expire after an hour.
    ),
}
```

Filter
------

```go
c := http.Client{
    // Disallow all requests to local/private addresses such as localhost, 10/8, etc.
    Transport: transport.Filter(http.DefaultTransport, transport.FilterLocal),
}
```

Intercept
---------
```go
c := http.Client{
    // Return an error on all status codes >=400.
    Transport: transport.Intercept(http.DefaultTransport, transport.HTTPError(false, 1024)),
}
```

Log
---

```go
c := http.Client{
    // Log request and response headers and body.
    Transport: transport.Log(http.DefaultTransport, buf, transport.LogAll),
}
```
