# Rate Limiter Package

This document provides comprehensive instructions on how to use the `rate` package and its accompanying HTTP middleware, `limithttp`.

This package is designed to be a highly generic, thread-safe, and context-aware rate limiting library for Go. It supports custom data matching, dynamic key generation, hierarchical subdomains, custom caching backends, and robust HTTP middleware integration.

---

## Table of Contents

1. Installation
2. Core Concepts
3. Type Registration and Key Generation
4. Standard Rate Limiting
5. Access Control Lists (ACLs)
6. Subdomains and Domain Isolation
7. Custom Key Generation and Error Handling
8. HTTP Middleware (`limithttp`)
9. Cache Backend Interface

---

## 1. Installation

Fetch the package using standard Go tools. Note that the path depends on your project structure, but standard installation looks like this:

```bash
go get github.com/Nigel2392/rate
```

---

## 2. Core Concepts

The architecture revolves around a few key interfaces and generic types:

* **`Limit`**: The core struct that handles checking, incrementing, and resetting rate limits.
* **`MatchType`**: A generic interface representing the data type you are rate limiting against (e.g., a user ID string, an IP address, or an `*http.Request`).
* **`ACL` (Access Control List)**: Interfaces that allow bypassing limits (Whitelists) or immediately rejecting requests (Blacklists).
* **`LimitBackend`**: The cache or storage interface responsible for keeping track of request counts and expirations.

---

## 3. Type Registration and Key Generation

Before you can rate limit a specific type of data, the package needs to know how to extract a unique session identifier (a slice of strings) from it. This ensures that the underlying cache keys are hashed consistently.

Built-in integer and standard types are already registered. For custom types or structs (like `*http.Request`), you must register them upon initialization.

### Registering a MatchType

Use `RegisterMatchType` for strict type matching, or `RegisterMatchKind` for underlying kind matching.

```go
package main

import (
    "net/http"
    "github.com/Nigel2392/rate"
)

// A custom type representing a user
type UserKey string

func init() {
    // Register a simple string alias
    rate.RegisterMatchType[UserKey](func(data UserKey) []string {
        return []string{string(data)}
    })

    // Register a complex struct pointer, extracting the IP address
    rate.RegisterMatchType[*http.Request](func(r *http.Request) []string {
        return []string{r.RemoteAddr}
    })
}
```

---

## 4. Standard Rate Limiting

The `Limit` struct requires a cache backend. For these examples, we assume you are using `cache.Default()` or a similar memory/Redis implementation that satisfies the `LimitBackend` interface.

### Basic Configuration and Execution

```go
package main

import (
    "context"
    "fmt"
    "time"

    "github.com/Nigel2392/rate"
    "github.com/Nigel2392/cache"
)

func main() {
    // 1. Initialize the rate limiter
    limiter := rate.Limit[rate.ACL[UserKey], rate.ACL[UserKey], UserKey]{
        Domain:      []string{"api", "v1", "users"},
        MaxAttempts: 5,
        Period:      1 * time.Minute,
        BanDuration: 1 * time.Hour,
        Cache:       cache.Default(),
    }

    ctx := context.Background()
    user := UserKey("user_id_12345")

    // 2. Check the rate limit
    err := limiter.Check(ctx, user)
    if err != nil {
        // err could be rate.ErrRateLimit, rate.ErrCache, or rate.ErrBlacklist
        fmt.Printf("Access denied: %v\n", err)
        return
    }

    fmt.Println("Access granted.")

    // 3. Reset the limit (optional)
    // You can manually reset a limit if a user completes a successful action
    // to clear their failure counter.
    _ = limiter.Reset(ctx, user)
}
```

---

## 5. Access Control Lists (ACLs)

ACLs govern priority access. If a matching type is in the Whitelist, it bypasses all caching and limit checks. If it is in the Blacklist, it is immediately rejected with `rate.ErrBlacklist`.

There are three provided ACL types:

* **`ListBasedACL`**: A thread-safe, slice-backed map of explicitly allowed or blocked data.
* **`FuncACL`**: A function that takes the context and data, returning a boolean.
* **`ContextACL`**: Relies on specific markers attached to the `context.Context`.

### Using Whitelists and Blacklists

```go
package main

import (
    "context"
    "time"

    "github.com/Nigel2392/rate"
    "github.com/Nigel2392/cache"
)

func ACLDemo() {
    // Define ACLs
    whitelist := rate.NewListACL[UserKey]("admin", "super_user")
    blacklist := rate.NewListACL[UserKey]("banned_user_1", "banned_user_2")

    // You can dynamically add to them later
    whitelist.Add("system_service")

    // Configure the limiter with the ACL pointers
    limiter := rate.Limit[*rate.ListBasedACL[UserKey], *rate.ListBasedACL[UserKey], UserKey]{
        Domain:      []string{"auth_service"},
        MaxAttempts: 3,
        Period:      15 * time.Minute,
        Cache:       cache.Default(),
        Whitelist:   whitelist,
        Blacklist:   blacklist,
    }

    ctx := context.Background()

    // Banned user -> Returns rate.ErrBlacklist immediately.
    _ = limiter.Check(ctx, UserKey("banned_user_1"))

    // Admin user -> Always passes, cache is never incremented.
    for i := 0; i < 100; i++ {
        _ = limiter.Check(ctx, UserKey("admin"))
    }
}
```

---

## 6. Subdomains and Domain Isolation

To avoid creating duplicate `Limit` configurations for endpoints that share standard rules but require isolated counters, you can use `Subdomain`.

A subdomain appends a prefix to the root domain, completely isolating the underlying cache keys. A user hitting the limit on a subdomain will not be locked out of the root domain or sibling subdomains.

```go
package main

import (
    "context"
    "github.com/Nigel2392/rate"
)

func SubdomainDemo(rootLimiter rate.Limit[rate.ACL[UserKey], rate.ACL[UserKey], UserKey]) {
    ctx := context.Background()
    user := UserKey("standard_user")

    // Create isolated subdomains
    loginLimiter := rootLimiter.Subdomain(ctx, "auth", "login")
    passwordResetLimiter := rootLimiter.Subdomain(ctx, "auth", "reset_password")

    // Exhaust the login limits
    _ = loginLimiter.Check(ctx, user)
    _ = loginLimiter.Check(ctx, user)

    // The user can still freely access the password reset domain
    err := passwordResetLimiter.Check(ctx, user)
    if err == nil {
        // This will pass.
    }
}
```

---

## 7. Custom Key Generation and Error Handling

If the default MD5 hashing of registered types does not fit your use case, or if you need to alter how cache backend failures are handled, you can provide override functions to `Limit`.

### Fallback/Error Continuation

By default, if the Cache backend fails (e.g., Redis goes down), the rate limiter fails closed (blocks the request). To fail open (allow requests if the cache drops), set `OnError`.

```go
limiter := rate.Limit[rate.ACL[UserKey], rate.ACL[UserKey], UserKey]{
    // ... basic config
    OnError: func(err error) bool {
        // Log the error internally
        // Return true to CONTINUE (fail open) when cache errors out
        return true 
    },
    KeyGen: func(domain []string, data UserKey) (string, error) {
        // Bypass the registry and define explicit cache keys manually
        return "custom_prefix_" + string(data), nil
    },
}
```

---

## 8. HTTP Middleware (`limithttp`)

The `limithttp` package provides a seamless wrapper around the core rate limiter, directly integrating it with standard `net/http` handlers.

### Standard Configuration

The generic types for the HTTP package evaluate against `*http.Request`. Ensure you have registered the `*http.Request` MatchType as shown in Section 3.

```go
package main

import (
    "net/http"
    "time"

    "github.com/Nigel2392/rate"
    "github.com/Nigel2392/rate/limithttp"
    "github.com/Nigel2392/cache"
)

func setupRouter() {
    limitCfg := &limithttp.HTTPLimit[limithttp.HTTPACL, limithttp.HTTPACL]{
        Limit: rate.Limit[limithttp.HTTPACL, limithttp.HTTPACL, *http.Request]{
            Domain:      []string{"public_api"},
            MaxAttempts: 100,
            Period:      1 * time.Minute,
            Cache:       cache.Default(),
        },
        // Optional: Override default 403 Forbidden handler
        Blocked: func(w http.ResponseWriter, r *http.Request) {
            w.Header().Set("Retry-After", "60")
            http.Error(w, "Too Many Requests", http.StatusTooManyRequests)
        },
        // Optional: Override internal error handler
        Error: func(w http.ResponseWriter, r *http.Request) {
            http.Error(w, "Internal Server Error", http.StatusInternalServerError)
        },
    }

    middleware := limithttp.RatelimitMiddleware(limitCfg)

    myHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        w.Write([]byte("Success"))
    })

    // Wrap the handler
    http.Handle("/api/data", middleware(myHandler))
}
```

### Context Resetting (Clearing limits on success)

A common pattern for authentication endpoints is to limit failed attempts, but clear the limit counter entirely upon a successful execution. You can do this by mutating the context inside your HTTP handler.

```go
func LoginHandler(w http.ResponseWriter, r *http.Request) {
    username := r.FormValue("username")
    password := r.FormValue("password")

    if !CheckCredentials(username, password) {
        http.Error(w, "Unauthorized", http.StatusUnauthorized)
        return // Limit counter remains and increases
    }

    // Success! Tell the middleware to reset the limit for this user/IP
    limithttp.ContextResetRateLimit(r.Context(), true)

    w.WriteHeader(http.StatusOK)
    w.Write([]byte("Login successful"))
}
```

When `ContextResetRateLimit(ctx, true)` is called, the middleware will automatically trigger `limit.Reset` after the HTTP handler finishes executing.

---

## 9. Cache Backend Interface

The package requires a backend that implements the `LimitBackend` interface. If you are writing a custom adapter (e.g., for Redis, Memcached, or a concurrent map), it must conform to the following:

```go
type LimitBackend interface {
    // Delete removes the key completely from the storage.
    Delete(c context.Context, key string) error
  
    // Expire sets the Time-To-Live (TTL) on an existing key.
    Expire(c context.Context, key string, ttl cache.Duration) error
  
    // Increment increases the counter for a key by the specified amount.
    // If the key does not exist, it must be created with the value of 'amount'.
    Increment(c context.Context, key string, amount int64) (int64, error)
}
```
