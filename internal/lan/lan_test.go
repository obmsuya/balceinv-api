package lan_test

import (
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chrisostomemataba/balceinv-api/internal/lan"
)

func firstPrivateAddress(t *testing.T) string {
	t.Helper()
	interfaceAddresses, addressesError := net.InterfaceAddrs()
	if addressesError != nil {
		t.Skipf("no interfaces: %v", addressesError)
	}
	for _, interfaceAddress := range interfaceAddresses {
		ipNetwork, isIpNetwork := interfaceAddress.(*net.IPNet)
		if isIpNetwork && ipNetwork.IP.To4() != nil && !ipNetwork.IP.IsLoopback() && ipNetwork.IP.IsPrivate() {
			return ipNetwork.IP.String()
		}
	}
	t.Skip("this machine has no private network address to test from")
	return ""
}

func canConnect(address string) bool {
	connection, dialError := net.DialTimeout("tcp", address, time.Second)
	if dialError != nil {
		return false
	}
	connection.Close()
	return true
}

func TestLanOffIsUnreachableFromTheNetworkAndLanOnIsReachable(t *testing.T) {
	networkAddress := firstPrivateAddress(t)

	offAddress := lan.ListenAddress("127.0.0.1:0", false, lan.Settings{LanEnabled: false})
	onAddress := lan.ListenAddress("127.0.0.1:0", false, lan.Settings{LanEnabled: true})
	if offAddress != "127.0.0.1:0" || onAddress != "0.0.0.0:0" {
		t.Fatalf("resolved off %q and on %q", offAddress, onAddress)
	}
	if lan.ListenAddress("10.1.2.3:9000", true, lan.Settings{LanEnabled: true}) != "10.1.2.3:9000" {
		t.Fatal("an explicit LISTEN_ADDR was overridden")
	}

	offListener, offListenError := net.Listen("tcp", offAddress)
	if offListenError != nil {
		t.Fatalf("listen off: %v", offListenError)
	}
	defer offListener.Close()
	go func() {
		for {
			connection, acceptError := offListener.Accept()
			if acceptError != nil {
				return
			}
			connection.Close()
		}
	}()
	_, offPort, _ := net.SplitHostPort(offListener.Addr().String())
	if !canConnect(net.JoinHostPort("127.0.0.1", offPort)) {
		t.Fatal("this computer could not reach its own server with LAN off")
	}
	if canConnect(net.JoinHostPort(networkAddress, offPort)) {
		t.Fatalf("with LAN off the server answered on %s", networkAddress)
	}

	onListener, onListenError := net.Listen("tcp", onAddress)
	if onListenError != nil {
		t.Fatalf("listen on: %v", onListenError)
	}
	defer onListener.Close()
	go func() {
		for {
			connection, acceptError := onListener.Accept()
			if acceptError != nil {
				return
			}
			connection.Close()
		}
	}()
	_, onPort, _ := net.SplitHostPort(onListener.Addr().String())
	if !canConnect(net.JoinHostPort(networkAddress, onPort)) {
		t.Fatalf("with LAN on the server did not answer on %s", networkAddress)
	}
}

func TestSettingsSurviveRestartsAndBadFilesMeanOff(t *testing.T) {
	dataDirectory := t.TempDir()
	if lan.LoadSettings(dataDirectory).LanEnabled {
		t.Fatal("LAN was on before anyone switched it on")
	}
	saveError := lan.SaveSettings(dataDirectory, lan.Settings{LanEnabled: true})
	if saveError != nil || !lan.LoadSettings(dataDirectory).LanEnabled {
		t.Fatalf("saving LAN on: %v", saveError)
	}
	os.WriteFile(filepath.Join(dataDirectory, "network.json"), []byte("{broken"), 0o600)
	if lan.LoadSettings(dataDirectory).LanEnabled {
		t.Fatal("a damaged settings file turned LAN on")
	}

	for _, networkUrl := range lan.NetworkUrls("0.0.0.0:8080") {
		if !strings.HasPrefix(networkUrl, "http://") || !strings.HasSuffix(networkUrl, ":8080") || strings.Contains(networkUrl, "127.0.0.1") {
			t.Fatalf("a network address looks wrong: %q", networkUrl)
		}
	}

	restartRequests := make(chan struct{}, 1)
	controller := lan.NewController(dataDirectory, "127.0.0.1:8080", false, restartRequests)
	status, setError := controller.SetEnabled(true)
	if setError != nil || !status.LanEnabled || !status.Restarting {
		t.Fatalf("switching LAN on returned %+v %v", status, setError)
	}
	select {
	case <-restartRequests:
	case <-time.After(2 * time.Second):
		t.Fatal("switching LAN on did not restart the listener")
	}

	fixedController := lan.NewController(dataDirectory, "10.0.0.5:8080", true, restartRequests)
	if _, fixedError := fixedController.SetEnabled(false); fixedError != lan.ErrLanFixedByConfig {
		t.Fatalf("a fixed LISTEN_ADDR could be switched: %v", fixedError)
	}
}
