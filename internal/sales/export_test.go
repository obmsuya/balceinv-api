package sales

import "net/http"

func UseFiscalTransport(transport http.RoundTripper) {
	fiscalTransport = transport
}
