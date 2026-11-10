package httpapi

import (
	"net"
	"net/http"
	"net/netip"
	"strings"
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

func parseTrustedProxies(value string) []netip.Prefix {
	var prefixes []netip.Prefix
	for _, item := range strings.Split(value, ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if prefix, err := netip.ParsePrefix(item); err == nil {
			prefixes = append(prefixes, prefix.Masked())
			continue
		}
		if address, err := netip.ParseAddr(item); err == nil {
			address = address.Unmap()
			bits := 128
			if address.Is4() {
				bits = 32
			}
			prefixes = append(prefixes, netip.PrefixFrom(address, bits))
		}
	}
	return prefixes
}

func clientIP(r *http.Request, trustedProxies []netip.Prefix) (string, bool) {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	peer, err := netip.ParseAddr(host)
	if err != nil {
		return r.RemoteAddr, true
	}
	peer = peer.Unmap()
	client := peer.String()
	trusted := false
	for _, prefix := range trustedProxies {
		if prefix.Contains(peer) {
			trusted = true
			break
		}
	}
	if !trusted {
		return client, true
	}

	values := r.Header.Values("X-Real-IP")
	if len(values) != 1 || strings.Contains(values[0], ",") {
		return "", false
	}
	address, err := netip.ParseAddr(strings.TrimSpace(values[0]))
	if err != nil || address.Zone() != "" {
		return "", false
	}
	return address.Unmap().String(), true
}
