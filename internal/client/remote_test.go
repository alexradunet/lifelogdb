package client

import (
	"net/http"
	"testing"
)

func TestRemoteDialIsBounded(t *testing.T) {
	c := Remote("http://127.0.0.1:7777/", "cli")
	tr, ok := c.hc.Transport.(*http.Transport)
	if !ok || tr.DialContext == nil || tr.TLSHandshakeTimeout != dialTimeout || dialTimeout <= 0 || tr.Proxy == nil {
		t.Fatalf("remote transport: %#v", c.hc.Transport)
	}
	if c.base != "http://127.0.0.1:7777" {
		t.Fatalf("base=%q", c.base)
	}
}
