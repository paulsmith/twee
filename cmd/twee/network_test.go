package main

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestParseStartNetworkCapture(t *testing.T) {
	opts, err := parseStartArgs([]string{
		"--trace", "session.twee",
		"--network-capture",
		"--publish-tcp", "127.0.0.1:8080=3000",
		"--",
		"server",
	})
	if err != nil {
		t.Fatal(err)
	}
	wantTrace, err := filepath.Abs("session.twee")
	if err != nil {
		t.Fatal(err)
	}
	if !opts.networkCapture || opts.trace != wantTrace || len(opts.publishTCP) != 1 {
		t.Fatalf("opts = %+v", opts)
	}
	if got := opts.publishTCP[0]; got.Listen != "127.0.0.1:8080" || got.Guest != "10.0.2.100:3000" {
		t.Fatalf("publication = %+v", got)
	}
}

func TestParseStartNetworkCaptureRequiresTrace(t *testing.T) {
	if _, err := parseStartArgs([]string{"--network-capture", "--", "server"}); err == nil || !strings.Contains(err.Error(), "requires --trace") {
		t.Fatalf("error = %v, want --trace requirement", err)
	}
}

func TestParseWrapNetworkCapture(t *testing.T) {
	opts, err := parseWrapArgs([]string{
		"--trace-out", "session.twee",
		"--network-capture",
		"--publish-tcp", "127.0.0.1:8080=3000",
		"--",
		"server",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !opts.NetworkCapture || opts.TracePath != "session.twee" || len(opts.PublishTCP) != 1 {
		t.Fatalf("opts = %+v", opts)
	}
	if got := opts.PublishTCP[0]; got.Listen != "127.0.0.1:8080" || got.Guest != "10.0.2.100:3000" {
		t.Fatalf("publication = %+v", got)
	}
}

func TestParseWrapNetworkCaptureRequiresTrace(t *testing.T) {
	if _, err := parseWrapArgs([]string{"--network-capture", "--", "server"}); err == nil || !strings.Contains(err.Error(), "requires --trace-out") {
		t.Fatalf("error = %v, want --trace-out requirement", err)
	}
	if _, err := parseWrapArgs([]string{"--publish-tcp", "127.0.0.1:8080=3000", "--", "server"}); err == nil || !strings.Contains(err.Error(), "requires --network-capture") {
		t.Fatalf("error = %v, want --network-capture requirement", err)
	}
}

func TestNetworkHelpUsesGuestPortWithoutPrivateAddress(t *testing.T) {
	for _, command := range []string{"start", "run", "wrap"} {
		help := commandRegistry[command].Usage
		if !strings.Contains(help, "LISTEN_IPV4:PORT=GUEST_PORT") {
			t.Errorf("%s help missing simplified publication syntax: %s", command, help)
		}
		if strings.Contains(help, netwrapGuestIPv4) {
			t.Errorf("%s help exposes private guest address: %s", command, help)
		}
	}
}

func TestParsePublishTCPInvalidMatrix(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{name: "missing separator", raw: "127.0.0.1:8080", want: "want LISTEN=GUEST_PORT"},
		{name: "extra separator", raw: "127.0.0.1:8080=3000=extra", want: "want LISTEN=GUEST_PORT"},
		{name: "empty listen", raw: "=3000", want: "want LISTEN=GUEST_PORT"},
		{name: "empty guest port", raw: "127.0.0.1:8080=", want: "want LISTEN=GUEST_PORT"},
		{name: "listen hostname", raw: "localhost:8080=3000", want: "listen address"},
		{name: "listen wildcard without address", raw: ":8080=3000", want: "host must be an IPv4 address"},
		{name: "listen IPv6", raw: "[::1]:8080=3000", want: "host must be an IPv4 address"},
		{name: "listen service", raw: "127.0.0.1:http=3000", want: "port must be a number"},
		{name: "listen port zero", raw: "127.0.0.1:0=3000", want: "1 through 65535"},
		{name: "listen port too large", raw: "127.0.0.1:65536=3000", want: "1 through 65535"},
		{name: "old guest address form", raw: "127.0.0.1:8080=10.0.2.100:3000", want: "guest port"},
		{name: "guest port zero", raw: "127.0.0.1:8080=0", want: "1 through 65535"},
		{name: "guest port too large", raw: "127.0.0.1:8080=65536", want: "1 through 65535"},
		{name: "guest service", raw: "127.0.0.1:8080=http", want: "must be a number"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			for _, command := range []struct {
				name string
				args []string
			}{
				{name: "run", args: []string{"--trace-out", "session.twee", "--network-capture", "--publish-tcp", test.raw, "--", "server"}},
				{name: "start", args: []string{"--trace", "session.twee", "--network-capture", "--publish-tcp", test.raw, "--", "server"}},
				{name: "wrap", args: []string{"--trace-out", "session.twee", "--network-capture", "--publish-tcp", test.raw, "--", "server"}},
			} {
				t.Run(command.name, func(t *testing.T) {
					var err error
					if command.name == "run" {
						_, err = parseRunArgs(command.args)
					} else if command.name == "start" {
						_, err = parseStartArgs(command.args)
					} else {
						_, err = parseWrapArgs(command.args)
					}
					if err == nil || !strings.Contains(err.Error(), test.want) {
						t.Fatalf("error = %v, want substring %q", err, test.want)
					}
				})
			}
		})
	}
}

func TestParsePublishTCPRejectsDuplicateListenAddress(t *testing.T) {
	_, err := parseStartArgs([]string{
		"--trace", "session.twee",
		"--network-capture",
		"--publish-tcp", "127.0.0.1:8080=3000",
		"--publish-tcp", "127.0.0.1:8080=3001",
		"--",
		"server",
	})
	if err == nil || !strings.Contains(err.Error(), "duplicate --publish-tcp listen address") {
		t.Fatalf("error = %v, want duplicate listen address", err)
	}
}

func TestParseNetworkCaptureFlagsPreservesTraceFlagName(t *testing.T) {
	for _, traceFlag := range []string{"--trace", "--trace-out"} {
		_, err := parseNetworkCaptureFlags(true, nil, "", traceFlag)
		if err == nil || err.Error() != "--network-capture requires "+traceFlag {
			t.Errorf("trace flag %q: error = %v", traceFlag, err)
		}
	}
}
