package rate

import "github.com/Nigel2392/errors"

var (
	CodeCacheError        errors.GoCode = "CacheError"
	CodeRateLimitExceeded errors.GoCode = "RateLimitExceeded"
	CodeBlacklistMatch    errors.GoCode = "Blacklisted"
	CodeInvalidSessionID  errors.GoCode = "InvalidSessionID"
)

var (
	ErrCache     = errors.New(CodeCacheError, "error while interacting with cache")
	ErrRateLimit = errors.New(CodeRateLimitExceeded, "rate limit has been exceeded")
	ErrBlacklist = errors.New(CodeBlacklistMatch, "user is blacklisted").WithCause(ErrRateLimit) // so errors.Is still returns true for convenience on [ErrRateLimit]
	ErrSessionID = errors.New(CodeInvalidSessionID, "session ID is not valid")
)
