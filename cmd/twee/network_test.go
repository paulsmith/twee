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
		"--publish-tcp", "127.0.0.1:8080=10.0.2.100:3000",
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

func TestParsePublishTCPInvalidMatrix(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{name: "missing separator", raw: "127.0.0.1:8080", want: "want LISTEN=GUEST"},
		{name: "extra separator", raw: "127.0.0.1:8080=10.0.2.100:3000=extra", want: "want LISTEN=GUEST"},
		{name: "empty listen", raw: "=10.0.2.100:3000", want: "want LISTEN=GUEST"},
		{name: "empty guest", raw: "127.0.0.1:8080=", want: "want LISTEN=GUEST"},
		{name: "listen hostname", raw: "localhost:8080=10.0.2.100:3000", want: "listen address"},
		{name: "listen wildcard without address", raw: ":8080=10.0.2.100:3000", want: "host must be an IPv4 address"},
		{name: "listen IPv6", raw: "[::1]:8080=10.0.2.100:3000", want: "host must be an IPv4 address"},
		{name: "listen service", raw: "127.0.0.1:http=10.0.2.100:3000", want: "port must be a number"},
		{name: "listen port zero", raw: "127.0.0.1:0=10.0.2.100:3000", want: "1 through 65535"},
		{name: "listen port too large", raw: "127.0.0.1:65536=10.0.2.100:3000", want: "1 through 65535"},
		{name: "guest hostname", raw: "127.0.0.1:8080=app:3000", want: "guest address"},
		{name: "guest IPv6", raw: "127.0.0.1:8080=[::1]:3000", want: "host must be an IPv4 address"},
		{name: "wrong guest IPv4", raw: "127.0.0.1:8080=10.0.2.101:3000", want: "host must be 10.0.2.100"},
		{name: "guest port zero", raw: "127.0.0.1:8080=10.0.2.100:0", want: "1 through 65535"},
		{name: "guest service", raw: "127.0.0.1:8080=10.0.2.100:http", want: "port must be a number"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			for _, command := range []struct {
				name string
				args []string
			}{
				{name: "run", args: []string{"--trace-out", "session.twee", "--network-capture", "--publish-tcp", test.raw, "--", "server"}},
				{name: "start", args: []string{"--trace", "session.twee", "--network-capture", "--publish-tcp", test.raw, "--", "server"}},
			} {
				t.Run(command.name, func(t *testing.T) {
					var err error
					if command.name == "run" {
						_, err = parseRunArgs(command.args)
					} else {
						_, err = parseStartArgs(command.args)
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
		"--publish-tcp", "127.0.0.1:8080=10.0.2.100:3000",
		"--publish-tcp", "127.0.0.1:8080=10.0.2.100:3001",
		"--",
		"server",
	})
	if err == nil || !strings.Contains(err.Error(), "duplicate --publish-tcp listen address") {
		t.Fatalf("error = %v, want duplicate listen address", err)
	}
}
