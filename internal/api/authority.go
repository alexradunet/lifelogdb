package api

import (
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
)

// LoopbackAddress validates the local-only serve address without DNS or service
// lookups. localhost binds deterministically to IPv4 loopback.
func LoopbackAddress(authority string) (string, error) {
	host, port, ok := loopbackAuthority(authority, true)
	if !ok {
		return "", fmt.Errorf("serve address %q: use a numeric loopback IP or localhost and a numeric port (0–65535)", authority)
	}
	if strings.ToLower(host) == "localhost" {
		host = "127.0.0.1"
	}
	return net.JoinHostPort(host, strconv.Itoa(port)), nil
}

func loopbackAuthority(authority string, allowZero bool) (string, int, bool) {
	host, text, err := net.SplitHostPort(authority)
	if err != nil || host == "" || text == "" {
		return "", 0, false
	}
	for _, c := range text {
		if c < '0' || c > '9' {
			return "", 0, false
		}
	}
	port, err := strconv.ParseUint(text, 10, 16)
	if err != nil || (!allowZero && port == 0) {
		return "", 0, false
	}
	if strings.ToLower(host) == "localhost" {
		if strings.HasPrefix(authority, "[") {
			return "", 0, false
		}
		return host, int(port), true
	}
	ip, err := netip.ParseAddr(host)
	if err != nil || ip.Zone() != "" || !ip.Unmap().IsLoopback() {
		return "", 0, false
	}
	// Brackets belong only to IPv6 literals, not IPv4 or names.
	if strings.HasPrefix(authority, "[") != ip.Is6() {
		return "", 0, false
	}
	return host, int(port), true
}

// NetworkAuthority wraps only socket serving; api.New remains usable by the
// in-process CLI/MCP. The listener's actual port includes ephemeral port binds.
func NetworkAuthority(addr net.Addr, next http.Handler) (http.Handler, error) {
	_, port, ok := loopbackAuthority(addr.String(), false)
	if !ok {
		return nil, fmt.Errorf("serve listener %q is not a bound loopback address", addr)
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authority := r.Host
		// HTTP's omitted port means 80, never the arbitrary listener port.
		// Bare IPv6 remains invalid: only a bracketed literal gets this default.
		if !strings.Contains(authority, ":") || strings.HasSuffix(authority, "]") {
			authority += ":80"
		}
		_, requestPort, allowed := loopbackAuthority(authority, false)
		// Go replaces Host with the absolute request-target's authority. No proxy
		// deployment is supported, so reject absolute-form instead of trusting it.
		if !allowed || requestPort != port || r.URL == nil || r.URL.IsAbs() || r.URL.Host != "" {
			http.Error(w, "HTTP authority refused", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	}), nil
}
