package httpkit

import "net/http"

// Internal service endpoints are explicit deployment settings. Keep LAN traffic
// direct even when the service account has a corporate HTTP_PROXY configured.
var serviceTransport = func() *http.Transport {
	t := http.DefaultTransport.(*http.Transport).Clone()
	t.Proxy = nil
	return t
}()

func InternalTransport() http.RoundTripper { return serviceTransport }
