package limitnet

import (
	"context"
	"net"

	"github.com/Nigel2392/rate"
)

func init() {
	// Register the match type for net.Conn.
	rate.RegisterMatchType(func(c net.Conn) []string {
		if c == nil || c.RemoteAddr() == nil {
			return []string{}
		}

		host, _, err := net.SplitHostPort(c.RemoteAddr().String())
		if err != nil {
			// Fallback if the address is not a standard host:port format (e.g., unix sockets)
			return []string{c.RemoteAddr().String()}
		}

		return []string{host}
	})
}

// Limit wraps the generic rate limit for raw network connections.
type Limit[ACL rate.ACL[net.Conn]] struct {
	rate.Limit[ACL, ACL, net.Conn]
	OnReject func(net.Conn, error)
}

// listener wraps a net.Listener to provide automatic rate limiting.
type listener[ACL rate.ACL[net.Conn]] struct {
	net.Listener
	limit *Limit[ACL]
	ctx   context.Context
}

// [NetLimit] takes a standard [net.Listener] and a [netlimit.Limit] configuration,
// returning a new net.Listener that automatically drops connections exceeding the limit.
func NetLimit[ACL rate.ACL[net.Conn]](ctx context.Context, l net.Listener, limit *Limit[ACL]) net.Listener {
	if limit == nil {
		panic("limitnet: NetLimit configuration cannot be nil")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return &listener[ACL]{
		Listener: l,
		limit:    limit,
		ctx:      ctx,
	}
}

// Accept overrides the underlying net.Listener.Accept method.
func (l *listener[ACL]) Accept() (net.Conn, error) {
	for {
		conn, err := l.Listener.Accept()
		if err != nil {
			return nil, err
		}

		checkErr := l.limit.Check(l.ctx, conn)
		if checkErr != nil {
			// Trigger optional callback for logging
			if l.limit.OnReject != nil {
				l.limit.OnReject(conn, checkErr)
			}

			// Close the connection immediately to free resources
			_ = conn.Close()

			// Continue listening for the next connection instead of returning an error
			// to the parent server loop, which would halt the entire server.
			continue
		}

		return conn, nil
	}
}
