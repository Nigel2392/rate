package limithttp

import (
	"context"
	"errors"
	"net/http"

	"github.com/Nigel2392/rate"
)

type HTTPACL = rate.ACL[*http.Request]

type Limit[WHITELIST, BLACKLIST HTTPACL] struct {
	rate.Limit[WHITELIST, BLACKLIST, *http.Request]
	Blocked func(http.ResponseWriter, *http.Request)
	Error   func(http.ResponseWriter, *http.Request) // called when the request is blocked
}

type rateLimitControlKey struct{}

func ContextResetRateLimit(c context.Context, b bool) context.Context {
	old, ok := c.Value(rateLimitControlKey{}).(*bool)
	if !ok {
		panic("rate limit middleware was not initialized")
	}
	*old = b
	return c
}

func RatelimitMiddleware[W, B HTTPACL](limit *Limit[W, B]) func(next http.Handler) http.Handler {
	if limit == nil {
		panic("no rate limit provided")
	}

	var (
		handleError = handleErrorDefault
		handleBlock = handleBlockDefault
	)
	if limit.Error != nil {
		handleError = limit.Error
	}
	if limit.Blocked != nil {
		handleBlock = limit.Blocked
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			resetRateLimit := new(bool)
			ctx := context.WithValue(
				r.Context(),
				rateLimitControlKey{},
				resetRateLimit,
			)

			newR := r.WithContext(ctx)
			err := limit.Check(newR.Context(), newR)

			switch {
			case errors.Is(err, rate.ErrRateLimit):
				handleBlock(w, newR)
				return
			case err != nil:
				handleError(w, newR)
				return
			}

			next.ServeHTTP(w, newR)

			if !*resetRateLimit {
				return
			}

			if err := limit.Reset(newR.Context(), newR); err != nil {
				handleError(w, newR)
			}
		})
	}
}

func handleErrorDefault(w http.ResponseWriter, req *http.Request) {
	http.Error(w, "Forbidden", http.StatusForbidden)
}

func handleBlockDefault(w http.ResponseWriter, req *http.Request) {
	http.Error(w, "Forbidden", http.StatusForbidden)
}
