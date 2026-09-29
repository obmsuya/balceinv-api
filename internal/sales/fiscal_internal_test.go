package sales

import (
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCloudEfdCallsOnlyReachThePublicInternet(t *testing.T) {
	originalTransport := fiscalTransport
	fiscalTransport = http.DefaultTransport
	t.Cleanup(func() { fiscalTransport = originalTransport })

	for _, blockedAddress := range []string{"127.0.0.1", "10.0.0.5", "192.168.1.20", "172.17.0.1", "169.254.169.254", "100.64.0.1", "::1", "fe80::1", "0.0.0.0"} {
		if IsPublicAddress(net.ParseIP(blockedAddress)) {
			t.Fatalf("%s counted as public", blockedAddress)
		}
	}
	for _, publicAddress := range []string{"8.8.8.8", "41.59.1.10", "2606:4700::1111"} {
		if !IsPublicAddress(net.ParseIP(publicAddress)) {
			t.Fatalf("%s counted as private", publicAddress)
		}
	}

	internalService := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Write([]byte("internal secret"))
	}))
	defer internalService.Close()

	cloudResponse, cloudError := newFiscalClient(true).Post(internalService.URL, "application/json", nil)
	if cloudError == nil {
		cloudResponse.Body.Close()
		t.Fatal("the cloud EFD client reached a loopback service")
	}
	desktopResponse, desktopError := newFiscalClient(false).Post(internalService.URL, "application/json", nil)
	if desktopError != nil {
		t.Fatalf("the desktop EFD client could not reach a device on this network: %v", desktopError)
	}
	desktopResponse.Body.Close()

	redirectingService := httptest.NewServer(http.RedirectHandler(internalService.URL, http.StatusFound))
	defer redirectingService.Close()
	redirectResponse, redirectError := newFiscalClient(false).Post(redirectingService.URL, "application/json", nil)
	if redirectError != nil || redirectResponse.StatusCode != http.StatusFound {
		t.Fatalf("the EFD client followed a redirect: %v %v", redirectResponse, redirectError)
	}
	redirectResponse.Body.Close()
}
