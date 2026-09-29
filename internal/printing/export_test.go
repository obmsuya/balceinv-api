package printing

import (
	"path/filepath"
	"testing"
)

var (
	FormatMoney   = formatMoney
	LeftRightText = leftRightText
)

func UseFakeDevices(t *testing.T, deviceDirectory string) {
	originalDevicePathFor := devicePathFor
	devicePathFor = func(portPath string) string {
		return filepath.Join(deviceDirectory, filepath.Base(portPath))
	}
	t.Cleanup(func() {
		devicePathFor = originalDevicePathFor
	})
}
