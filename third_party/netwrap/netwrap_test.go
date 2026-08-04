package netwrap

import (
	"errors"
	"runtime"
	"testing"
)

func TestConfigValidation(t *testing.T) {
	tests := []struct {
		name string
		cfg  Config
	}{
		{name: "missing command", cfg: Config{PCAPPath: "a", FlowLogPath: "b"}},
		{name: "missing pcap", cfg: Config{Command: []string{"true"}, FlowLogPath: "b"}},
		{name: "same output", cfg: Config{Command: []string{"true"}, PCAPPath: "a", FlowLogPath: "a"}},
		{name: "bad mtu", cfg: Config{Command: []string{"true"}, PCAPPath: "a", FlowLogPath: "b", MTU: 100}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := test.cfg.normalized(); err == nil {
				t.Fatal("normalized() succeeded; want an error")
			}
		})
	}
}

func TestConfigDefaults(t *testing.T) {
	cfg, err := (Config{Command: []string{"true"}, PCAPPath: "a", FlowLogPath: "b"}).normalized()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.MTU != defaultMTU || cfg.MaxPCAPBytes != defaultMaxCaptureBytes {
		t.Fatalf("defaults = MTU %d, PCAP %d", cfg.MTU, cfg.MaxPCAPBytes)
	}
}

func TestPublicationListenAddress(t *testing.T) {
	base := Config{Command: []string{"true"}, PCAPPath: "a", FlowLogPath: "b"}
	tests := []struct {
		listen string
		want   string
	}{
		{listen: ":8080", want: "127.0.0.1:8080"},
		{listen: "127.0.0.1:8080", want: "127.0.0.1:8080"},
		{listen: "0.0.0.0:8080", want: "0.0.0.0:8080"},
	}
	for _, test := range tests {
		cfg := base
		cfg.PublishTCP = []TCPPublication{{Listen: test.listen, Guest: "10.0.2.100:80"}}
		normalized, err := cfg.normalized()
		if err != nil {
			t.Fatalf("Listen %q: %v", test.listen, err)
		}
		if got := normalized.PublishTCP[0].Listen; got != test.want {
			t.Errorf("Listen %q normalized to %q; want %q", test.listen, got, test.want)
		}
	}
}

func TestNormalizationDoesNotMutatePublicationInput(t *testing.T) {
	publications := []TCPPublication{{Listen: ":8080", Guest: "10.0.2.100:80"}}
	_, err := (Config{
		Command: []string{"true"}, PCAPPath: "a", FlowLogPath: "b", PublishTCP: publications,
	}).normalized()
	if err != nil {
		t.Fatal(err)
	}
	if publications[0].Listen != ":8080" {
		t.Fatalf("caller publication changed to %q", publications[0].Listen)
	}
}

func TestPublicationGuestAddressValidation(t *testing.T) {
	base := Config{Command: []string{"true"}, PCAPPath: "a"}
	for _, guest := range []string{"localhost:80", "10.0.2.101:80", "10.0.2.100:http", "10.0.2.100:0", "missing-port"} {
		cfg := base
		cfg.PublishTCP = []TCPPublication{{Listen: "127.0.0.1:8080", Guest: guest}}
		if _, err := cfg.normalized(); err == nil {
			t.Errorf("Guest %q normalized successfully", guest)
		}
	}
}

func TestUnsupportedHostFailsClosed(t *testing.T) {
	if runtime.GOOS == "linux" {
		t.Skip("Linux has a real backend")
	}
	if err := Preflight(); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("Preflight() error = %v", err)
	}
}
