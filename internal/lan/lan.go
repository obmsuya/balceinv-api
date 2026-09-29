package lan

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	settingsFileName = "network.json"
	loopbackHost     = "127.0.0.1"
	everyInterface   = "0.0.0.0"
	restartDelay     = 400 * time.Millisecond
)

var ErrLanFixedByConfig = errors.New("the listen address is fixed by LISTEN_ADDR, so the network switch is off")

type Settings struct {
	LanEnabled bool `json:"lan_enabled"`
}

type StatusView struct {
	LanAvailable  bool     `json:"lan_available"`
	LanEnabled    bool     `json:"lan_enabled"`
	ListenAddress string   `json:"listen_address"`
	LanUrls       []string `json:"lan_urls"`
	Restarting    bool     `json:"restarting"`
}

func LoadSettings(dataDirectory string) Settings {
	settingsBytes, readError := os.ReadFile(filepath.Join(dataDirectory, settingsFileName))
	if readError != nil {
		return Settings{}
	}
	loadedSettings := Settings{}
	unmarshalError := json.Unmarshal(settingsBytes, &loadedSettings)
	if unmarshalError != nil {
		slog.Warn("network settings unreadable, LAN stays off", "error", unmarshalError)
		return Settings{}
	}
	return loadedSettings
}

func SaveSettings(dataDirectory string, newSettings Settings) error {
	settingsBytes, marshalError := json.Marshal(newSettings)
	if marshalError != nil {
		return marshalError
	}
	settingsPath := filepath.Join(dataDirectory, settingsFileName)
	writingPath := settingsPath + ".writing"
	writeError := os.WriteFile(writingPath, settingsBytes, 0o600)
	if writeError != nil {
		return fmt.Errorf("could not save the network setting: %w", writeError)
	}
	renameError := os.Rename(writingPath, settingsPath)
	if renameError != nil {
		return fmt.Errorf("could not save the network setting: %w", renameError)
	}
	return nil
}

func ListenAddress(configuredAddress string, isExplicit bool, currentSettings Settings) string {
	if isExplicit {
		return configuredAddress
	}
	_, port, splitError := net.SplitHostPort(configuredAddress)
	if splitError != nil {
		port = "8080"
	}
	if currentSettings.LanEnabled {
		return net.JoinHostPort(everyInterface, port)
	}
	return net.JoinHostPort(loopbackHost, port)
}

func NetworkUrls(listenAddress string) []string {
	_, port, splitError := net.SplitHostPort(listenAddress)
	if splitError != nil {
		return []string{}
	}

	candidateAddresses := []string{}
	preferredAddress := routedAddress()
	if preferredAddress != "" {
		candidateAddresses = append(candidateAddresses, preferredAddress)
	}
	interfaceList, interfacesError := net.Interfaces()
	if interfacesError == nil {
		for _, networkInterface := range interfaceList {
			isUsable := networkInterface.Flags&net.FlagUp != 0 && networkInterface.Flags&net.FlagLoopback == 0
			if !isUsable || isVirtualInterface(networkInterface.Name) {
				continue
			}
			interfaceAddresses, addressesError := networkInterface.Addrs()
			if addressesError != nil {
				continue
			}
			for _, interfaceAddress := range interfaceAddresses {
				ipNetwork, isIpNetwork := interfaceAddress.(*net.IPNet)
				if !isIpNetwork {
					continue
				}
				ipv4Address := ipNetwork.IP.To4()
				if ipv4Address == nil || !ipv4Address.IsPrivate() {
					continue
				}
				candidateAddresses = append(candidateAddresses, ipv4Address.String())
			}
		}
	}

	seenAddresses := map[string]bool{}
	networkUrls := []string{}
	for _, candidateAddress := range candidateAddresses {
		if seenAddresses[candidateAddress] {
			continue
		}
		seenAddresses[candidateAddress] = true
		networkUrls = append(networkUrls, "http://"+net.JoinHostPort(candidateAddress, port))
	}
	return networkUrls
}

var virtualInterfaceMarkers = []string{"bridge", "docker", "br-", "veth", "vmnet", "vboxnet", "virbr", "utun", "vethernet", "virtual", "vmware", "hyper-v", "wsl"}

func isVirtualInterface(interfaceName string) bool {
	lowerName := strings.ToLower(interfaceName)
	for _, marker := range virtualInterfaceMarkers {
		if strings.Contains(lowerName, marker) {
			return true
		}
	}
	return false
}

func routedAddress() string {
	probeConnection, dialError := net.Dial("udp", "8.8.8.8:80")
	if dialError != nil {
		return ""
	}
	defer probeConnection.Close()
	localAddress, isUdpAddress := probeConnection.LocalAddr().(*net.UDPAddr)
	if !isUdpAddress {
		return ""
	}
	routedIpv4 := localAddress.IP.To4()
	if routedIpv4 == nil || !routedIpv4.IsPrivate() {
		return ""
	}
	return routedIpv4.String()
}

type Controller struct {
	dataDirectory   string
	listenAddress   string
	isExplicit      bool
	restartRequests chan<- struct{}
	mutex           sync.Mutex
	restarting      bool
}

func NewController(dataDirectory string, listenAddress string, isExplicit bool, restartRequests chan<- struct{}) *Controller {
	return &Controller{
		dataDirectory:   dataDirectory,
		listenAddress:   listenAddress,
		isExplicit:      isExplicit,
		restartRequests: restartRequests,
	}
}

func (controller *Controller) Status() StatusView {
	controller.mutex.Lock()
	isRestarting := controller.restarting
	controller.mutex.Unlock()

	listenHost, _, splitError := net.SplitHostPort(controller.listenAddress)
	isListeningOnNetwork := splitError == nil && (listenHost == everyInterface || listenHost == "" || listenHost == "::")
	networkUrls := []string{}
	if isListeningOnNetwork {
		networkUrls = NetworkUrls(controller.listenAddress)
	}
	statusView := StatusView{
		LanAvailable:  !controller.isExplicit,
		LanEnabled:    LoadSettings(controller.dataDirectory).LanEnabled,
		ListenAddress: controller.listenAddress,
		LanUrls:       networkUrls,
		Restarting:    isRestarting,
	}
	return statusView
}

func (controller *Controller) SetEnabled(isEnabled bool) (StatusView, error) {
	if controller.isExplicit {
		return StatusView{}, ErrLanFixedByConfig
	}
	saveError := SaveSettings(controller.dataDirectory, Settings{LanEnabled: isEnabled})
	if saveError != nil {
		return StatusView{}, saveError
	}

	wantedAddress := ListenAddress(controller.listenAddress, false, Settings{LanEnabled: isEnabled})
	needsRestart := wantedAddress != controller.listenAddress && controller.restartRequests != nil
	if needsRestart {
		controller.mutex.Lock()
		controller.restarting = true
		controller.mutex.Unlock()
		slog.Info("network setting changed, restarting the listener", "lanEnabled", isEnabled, "address", wantedAddress)
		go func() {
			time.Sleep(restartDelay)
			controller.restartRequests <- struct{}{}
		}()
	}
	return controller.Status(), nil
}
