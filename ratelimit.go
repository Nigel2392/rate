package rate

import (
	"context"
	"errors"
	"time"

	"github.com/Nigel2392/cache"
	"github.com/Nigel2392/rate/internal"
)

const (
	DEFAULT_CONTINUE_ON_ERROR = false
	DEFAULT_BAN_DURATION      = 1 * time.Hour
	DEFAULT_TRACKING_PERIOD   = 1 * time.Minute
	DEFAULT_MAX_ATTEMPTS      = 5
)

var (
	_ RateLimit[any] = (*Limit[ACL[any], ACL[any], any])(nil)
	_ RateLimit[any] = (*domainSpecific[ACL[any], ACL[any], any])(nil)
)

type RateLimit[DATA MatchType] interface {
	Check(ctx context.Context, data DATA) error
	Reset(ctx context.Context, data DATA) error
}

type LimitBackend interface {
	Delete(c context.Context, key string) error
	Expire(c context.Context, key string, ttl cache.Duration) error
	Increment(c context.Context, key string, amount int64) (int64, error)
}

type Limit[WHITELIST, BLACKLIST ACL[DATA], DATA MatchType] struct {
	Domain      []string
	MaxAttempts int64
	BanDuration time.Duration
	Period      time.Duration
	Cache       LimitBackend
	Whitelist   WHITELIST
	Blacklist   BLACKLIST
	OnError     func(err error) (_continue bool)
	KeyGen      func(domain []string, data DATA) (string, error)
}

func (l *Limit[W, B, DATA]) Check(ctx context.Context, data DATA) error {
	return l.checkWithPrefix(ctx, l.Domain, data)
}

func (l *Limit[W, B, DATA]) Reset(ctx context.Context, data DATA) error {
	return l.resetWithPrefix(ctx, l.Domain, data)
}

func (l *Limit[W, B, DATA]) Subdomain(ctx context.Context, prefix ...string) RateLimit[DATA] {
	return newSubdomain(*l, l.Domain, prefix)
}

func (l *Limit[W, B, DATA]) GetKey(data DATA) (string, error) {
	return l.newKey(l.Domain, data)
}

func (l *Limit[W, B, DATA]) newKey(prefix []string, data DATA) (string, error) {
	if l.KeyGen != nil {
		return l.KeyGen(prefix, data)
	}
	return hashKey(prefix, data)
}

func (l *Limit[W, B, DATA]) checkWithPrefix(ctx context.Context, prefix []string, data DATA) error {
	if !internal.IsZero(l.Whitelist) {
		whitelisted, err := l.Whitelist.Match(ctx, data)
		if err != nil {
			return err
		}
		if whitelisted {
			return nil
		}
	}

	if !internal.IsZero(l.Blacklist) {
		blacklisted, err := l.Blacklist.Match(ctx, data)
		if err != nil {
			return err
		}
		if blacklisted {
			return ErrBlacklist
		}
	}

	cacheKey, err := l.newKey(prefix, data)
	if err != nil {
		return err
	}

	var (
		expire      time.Duration = DEFAULT_TRACKING_PERIOD
		banTime     time.Duration = DEFAULT_BAN_DURATION
		maxAttempts int64         = DEFAULT_MAX_ATTEMPTS
	)

	if l.Period != 0 {
		expire = l.Period
	}

	if l.BanDuration != 0 {
		banTime = l.BanDuration
	}

	if l.MaxAttempts != 0 {
		maxAttempts = l.MaxAttempts
	}

	if l.Cache == nil {
		l.Cache = cache.Default()
	}

	var attempts int64
	attempts, err = l.Cache.Increment(ctx, cacheKey, 1)
	if err != nil && !l.shouldContinue(err) {
		goto cacheError
	}

	if attempts == 1 { // first connect
		err = l.Cache.Expire(ctx, cacheKey, expire)
		if err != nil && !l.shouldContinue(err) {
			goto cacheError
		}
	}

	if err := ctx.Err(); err != nil && !l.shouldContinue(err) {
		return err
	}

	// the ban hammer has dropped
	if attempts > maxAttempts {

		result := ErrRateLimit.Wrapf(
			"rate limit exceeded %d/%d attempts for %q",
			attempts, maxAttempts, cacheKey,
		)

		if banTime < 0 {
			return result
		}

		result = result.Wrapf("Banning %q for %s", cacheKey, banTime)
		if cacheErr := l.Cache.Expire(ctx, cacheKey, banTime); cacheErr != nil {
			return errors.Join(result, ErrCache.WithCause(cacheErr))
		}

		return result
	}

	return nil

cacheError:
	return ErrCache.WithCause(err)
}

func (l *Limit[W, B, DATA]) shouldContinue(err error) bool {
	if l.OnError == nil {
		return DEFAULT_CONTINUE_ON_ERROR
	}
	return l.OnError(err)
}

func (l *Limit[W, B, DATA]) resetWithPrefix(ctx context.Context, prefix []string, data DATA) error {
	var cacheKey, err = l.newKey(prefix, data)
	if err != nil {
		return err
	}

	err = l.Cache.Delete(ctx, cacheKey)
	if err != nil && !errors.Is(err, cache.ErrItemNotFound) {
		return err
	}

	return nil
}
