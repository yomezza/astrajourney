package httpkit

import (
	"net/http"
	"net/url"
	"testing"
)

func TestInternalTransportDirect(t *testing.T) {
	transport := InternalTransport().(*http.Transport)
	if transport.Proxy != nil {
		u, _ := url.Parse("http://192.168.111.44:8082")
		proxy, err := transport.Proxy(&http.Request{URL: u})
		if err != nil || proxy != nil {
			t.Fatal("internal LAN endpoints must not use an environment proxy")
		}
	}
}
