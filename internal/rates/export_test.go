package rates

import (
	"net/http"
	"time"
)

func UseProvider(url string, transport http.RoundTripper, retryAfter time.Duration) {
	providerUrl = url
	providerTransport = transport
	retryAfterFailure = retryAfter
}
