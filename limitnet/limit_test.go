package limitnet_test

import (
	"context"
	"errors"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Nigel2392/cache"
	"github.com/Nigel2392/rate"
	"github.com/Nigel2392/rate/limitnet"
)

func TestLimit_StandardAccept(t *testing.T) {
	limitCfg := &limitnet.Limit[rate.ACL[net.Conn]]{
		Limit: rate.Limit[rate.ACL[net.Conn], rate.ACL[net.Conn], net.Conn]{
			Domain:      []string{"tcp_standard"},
			MaxAttempts: 2, // Allow exactly 2 connections
			Cache:       cache.NewMemoryCache(1 * time.Minute),
		},
	}

	// Create a local TCP listener on a random port
	rawListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to create listener: %v", err)
	}
	defer rawListener.Close()

	// Wrap it with our rate limiter
	rateListener := limitnet.NetLimit(context.Background(), rawListener, limitCfg)
	addr := rateListener.Addr().String()

	// Start a background goroutine to accept connections
	var acceptedConns int32
	go func() {
		for {
			conn, acceptErr := rateListener.Accept()
			if acceptErr != nil {
				return // Listener closed
			}
			atomic.AddInt32(&acceptedConns, 1)
			_ = conn.Close() // Immediately close after accepting for the test
		}
	}()

	// Simulate 5 client connection attempts
	for i := 0; i < 5; i++ {
		conn, dialErr := net.Dial("tcp", addr)
		if dialErr == nil {
			_ = conn.Close()
		}
		time.Sleep(10 * time.Millisecond) // Allow accept loop to process
	}

	// Because MaxAttempts is 2, the wrapped listener should have silently dropped the other 3
	finalAccepted := atomic.LoadInt32(&acceptedConns)
	if finalAccepted != 2 {
		t.Fatalf("expected exactly 2 accepted connections, got %d", finalAccepted)
	}
}

func TestLimit_WhitelistACL(t *testing.T) {
	// Custom ACL that allows all connections from local IPv6 loopback
	var localWhitelist rate.FuncACL[net.Conn] = func(_ context.Context, c net.Conn) (bool, error) {
		host, _, err := net.SplitHostPort(c.RemoteAddr().String())
		if err != nil {
			return false, err
		}
		return host == "::1", nil
	}

	limitCfg := &limitnet.Limit[rate.ACL[net.Conn]]{
		Limit: rate.Limit[rate.ACL[net.Conn], rate.ACL[net.Conn], net.Conn]{
			Domain:      []string{"tcp_whitelist"},
			MaxAttempts: 1,
			Cache:       cache.NewMemoryCache(1 * time.Minute),
			Whitelist:   localWhitelist,
		},
	}

	rawListener, err := net.Listen("tcp", "[::1]:0") // Force IPv6 loopback
	if err != nil {
		t.Fatalf("failed to create listener: %v", err)
	}
	defer rawListener.Close()

	rateListener := limitnet.NetLimit(context.Background(), rawListener, limitCfg)
	addr := rateListener.Addr().String()

	var acceptedConns int32
	go func() {
		for {
			conn, acceptErr := rateListener.Accept()
			if acceptErr != nil {
				return
			}
			atomic.AddInt32(&acceptedConns, 1)
			_ = conn.Close()
		}
	}()

	// Simulate 10 client connection attempts
	for i := 0; i < 10; i++ {
		conn, dialErr := net.Dial("tcp", addr)
		if dialErr == nil {
			_ = conn.Close()
		}
		time.Sleep(5 * time.Millisecond)
	}

	// Since IPv6 localhost is whitelisted, all 10 connections should pass
	finalAccepted := atomic.LoadInt32(&acceptedConns)
	if finalAccepted != 10 {
		t.Fatalf("expected exactly 10 accepted connections due to whitelist, got %d", finalAccepted)
	}
}

func TestLimit_RejectionCallback(t *testing.T) {
	var rejections int32

	limitCfg := &limitnet.Limit[rate.ACL[net.Conn]]{
		Limit: rate.Limit[rate.ACL[net.Conn], rate.ACL[net.Conn], net.Conn]{
			Domain:      []string{"tcp_callback"},
			MaxAttempts: 1,
			Cache:       cache.NewMemoryCache(1 * time.Minute),
		},
		OnReject: func(c net.Conn, err error) {
			if errors.Is(err, rate.ErrRateLimit) {
				atomic.AddInt32(&rejections, 1)
			}
		},
	}

	rawListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to create listener: %v", err)
	}
	defer rawListener.Close()

	rateListener := limitnet.NetLimit(context.Background(), rawListener, limitCfg)
	addr := rateListener.Addr().String()

	go func() {
		for {
			conn, acceptErr := rateListener.Accept()
			if acceptErr != nil {
				return
			}
			_ = conn.Close()
		}
	}()

	var wg sync.WaitGroup
	// Fire 6 connection attempts concurrently
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			conn, dialErr := net.Dial("tcp", addr)
			if dialErr == nil {
				_ = conn.Close()
			}
		}()
	}

	wg.Wait()
	time.Sleep(50 * time.Millisecond) // Allow callbacks to finish firing

	// 1 attempt accepted, 5 attempts rejected and triggered the callback
	finalRejections := atomic.LoadInt32(&rejections)
	if finalRejections != 5 {
		t.Fatalf("expected exactly 5 rejections caught by callback, got %d", finalRejections)
	}
}
