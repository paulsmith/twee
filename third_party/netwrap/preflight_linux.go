//go:build linux

package netwrap

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// Preflight checks the host settings that netwrap needs. Run still fails
// closed if a setting changes after this check.
func Preflight() error {
	if err := checkPositiveSysctl("/proc/sys/user/max_user_namespaces", "user namespaces are disabled"); err != nil {
		return err
	}
	if data, err := os.ReadFile("/proc/sys/kernel/unprivileged_userns_clone"); err == nil && strings.TrimSpace(string(data)) == "0" {
		return fmt.Errorf("netwrap preflight: unprivileged user namespaces are disabled by kernel.unprivileged_userns_clone")
	}
	f, err := os.OpenFile("/dev/net/tun", os.O_RDWR, 0)
	if err != nil {
		return fmt.Errorf("netwrap preflight: /dev/net/tun is not accessible: %w", err)
	}
	return f.Close()
}

func checkPositiveSysctl(path, message string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("netwrap preflight: cannot read %s: %w", path, err)
	}
	n, err := strconv.ParseUint(strings.TrimSpace(string(data)), 10, 64)
	if err != nil {
		return fmt.Errorf("netwrap preflight: cannot parse %s: %w", path, err)
	}
	if n == 0 {
		return fmt.Errorf("netwrap preflight: %s", message)
	}
	return nil
}
