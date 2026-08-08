package main

import (
	"fmt"
	"net"
	"strconv"
	"strings"

	"github.com/paulsmith/twee/internal/engine"
)

const netwrapGuestIPv4 = "10.0.2.100"

func parseTCPPublications(raws []string) ([]engine.TCPPublication, error) {
	publications := make([]engine.TCPPublication, 0, len(raws))
	seen := make(map[string]struct{}, len(raws))
	for _, raw := range raws {
		publication, err := parseTCPPublication(raw)
		if err != nil {
			return nil, err
		}
		if _, exists := seen[publication.Listen]; exists {
			return nil, fmt.Errorf("duplicate --publish-tcp listen address %q", publication.Listen)
		}
		seen[publication.Listen] = struct{}{}
		publications = append(publications, publication)
	}
	return publications, nil
}

func parseTCPPublication(raw string) (engine.TCPPublication, error) {
	listen, guestPortText, ok := strings.Cut(raw, "=")
	if !ok || listen == "" || guestPortText == "" || strings.Contains(guestPortText, "=") {
		return engine.TCPPublication{}, fmt.Errorf("bad --publish-tcp value %q (want LISTEN=GUEST_PORT)", raw)
	}
	listenAddress, err := parseIPv4TCPAddress("listen", listen)
	if err != nil {
		return engine.TCPPublication{}, err
	}
	guestPort, err := strconv.Atoi(guestPortText)
	if err != nil || guestPort < 1 || guestPort > 65535 {
		return engine.TCPPublication{}, fmt.Errorf("bad --publish-tcp guest port %q: must be a number from 1 through 65535", guestPortText)
	}
	guestAddress := net.JoinHostPort(netwrapGuestIPv4, strconv.Itoa(guestPort))
	return engine.TCPPublication{Listen: listenAddress, Guest: guestAddress}, nil
}

func parseIPv4TCPAddress(role, raw string) (string, error) {
	host, portText, err := net.SplitHostPort(raw)
	if err != nil {
		return "", fmt.Errorf("bad --publish-tcp %s address %q: %w", role, raw, err)
	}
	if host == "" || strings.Contains(host, ":") || net.ParseIP(host).To4() == nil {
		return "", fmt.Errorf("bad --publish-tcp %s address %q: host must be an IPv4 address", role, raw)
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port < 1 || port > 65535 {
		return "", fmt.Errorf("bad --publish-tcp %s address %q: port must be a number from 1 through 65535", role, raw)
	}
	return net.JoinHostPort(host, strconv.Itoa(port)), nil
}
