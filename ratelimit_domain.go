package rate

import "context"

type domainSpecific[W, B ACL[DATA], DATA MatchType] struct {
	d []string
	l Limit[W, B, DATA]
}

//	func Namespace[W, B ACL[DATA], DATA MatchType](limit Limit[W, B, DATA], subdomain ...string) RateLimit[DATA] {
//		return newSubdomain(limit, limit.Domain, subdomain)
//	}

func newSubdomain[W, B ACL[DATA], DATA MatchType](l Limit[W, B, DATA], root, sub []string) *domainSpecific[W, B, DATA] {
	if len(sub) == 0 {
		panic("prefix must be specified to namespace a ratelimit.")
	}

	np := make([]string, len(root)+len(sub))
	n := copy(np[0:], sub)
	copy(np[n:], root)

	return &domainSpecific[W, B, DATA]{np, l}
}

func (l *domainSpecific[W, B, DATA]) Check(ctx context.Context, data DATA) error {
	return l.l.checkWithPrefix(ctx, l.d, data)
}

func (l *domainSpecific[W, B, DATA]) Reset(ctx context.Context, data DATA) error {
	return l.l.resetWithPrefix(ctx, l.d, data)
}

func (l *domainSpecific[W, B, DATA]) Subdomain(ctx context.Context, prefix ...string) RateLimit[DATA] {
	return newSubdomain(l.l, l.d, prefix)
}
