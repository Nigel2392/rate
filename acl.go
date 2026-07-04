package rate

import (
	"context"
	"sync"
)

var (
	_ ACL[any] = (*ContextACL[any])(nil)
	_ ACL[any] = (*FuncACL[any])(nil)
	_ ACL[any] = (*ListBasedACL[any])(nil)
)

type ACL[DATA MatchType] interface {
	Match(context.Context, DATA) (bool, error)
}

type contextACLKey[DATA MatchType] struct {
	id     string
	marked bool
}

type ContextACL[DATA MatchType] struct {
	id string
}

func (c *ContextACL[DATA]) Mark(ctx context.Context, mark bool) context.Context {
	return context.WithValue(ctx, contextACLKey[DATA]{id: c.id}, mark)
}

func (c *ContextACL[DATA]) Match(ctx context.Context, key DATA) (bool, error) {
	v, ok := ctx.Value(contextACLKey[DATA]{id: c.id}).(bool)
	if !ok {
		return false, nil
	}
	return v, nil
}

type ListBasedACL[DATA MatchType] struct {
	Include []DATA
	acl     map[MatchType]struct{}
	mu      sync.Mutex
}

func NewListACL[DATA MatchType](d ...DATA) *ListBasedACL[DATA] {
	var l = &ListBasedACL[DATA]{
		Include: make([]DATA, 0, len(d)),
	}
	l.check(d)
	return l
}

func (c *ListBasedACL[DATA]) Add(d ...DATA) *ListBasedACL[DATA] {
	if len(d) == 0 {
		return c // nothing to add, so return self for convenience
	}
	c.check(d)
	return c
}

func (c *ListBasedACL[DATA]) Match(_ context.Context, d DATA) (bool, error) {
	c.check(nil)
	_, ok := c.acl[d]
	return ok, nil
}

func (c *ListBasedACL[DATA]) check(d []DATA) {
	if len(c.acl) != len(c.Include) && len(c.Include) > 0 || len(d) > 0 {
		c.mu.Lock()
		defer c.mu.Unlock()

		if len(d) > 0 {
			c.Include = append(c.Include, d...)
		}

		var m = make(map[MatchType]struct{}, len(c.Include))
		for _, k := range c.Include {
			m[k] = struct{}{}
		}

		c.acl = m
	}
}

type FuncACL[DATA MatchType] func(_ context.Context, d DATA) (bool, error)

func (f FuncACL[DATA]) Match(ctx context.Context, d DATA) (bool, error) {
	return f(ctx, d)
}
