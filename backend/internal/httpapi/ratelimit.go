package httpapi

import (
	"net"
	"net/http"
	"sync"
	"time"
)

type loginBucket struct {
	tokens float64
	at     time.Time
}

type loginLimiter struct {
	mu      sync.Mutex
	buckets map[string]loginBucket
}

func newLoginLimiter() *loginLimiter { return &loginLimiter{buckets: make(map[string]loginBucket)} }

func (l *loginLimiter) allow(key string) bool {
	now := time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()
	bucket, ok := l.buckets[key]
	if !ok {
		bucket = loginBucket{tokens: 5, at: now}
	}
	bucket.tokens += now.Sub(bucket.at).Minutes()
	if bucket.tokens > 5 {
		bucket.tokens = 5
	}
	bucket.at = now
	if bucket.tokens < 1 {
		l.buckets[key] = bucket
		return false
	}
	bucket.tokens--
	l.buckets[key] = bucket
	if len(l.buckets) > 4096 {
		for address, item := range l.buckets {
			if now.Sub(item.at) > 10*time.Minute {
				delete(l.buckets, address)
			}
		}
	}
	return true
}

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil {
		return host
	}
	return r.RemoteAddr
}
