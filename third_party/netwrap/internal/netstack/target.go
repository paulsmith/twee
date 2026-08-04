package netstack

import "time"

const privateDNSDestination = "10.0.2.3:53"

// Direction is the direction at the TUN boundary.
type Direction string

const (
	GuestToHost Direction = "guest_to_host"
	HostToGuest Direction = "host_to_guest"
)

// Flow is one completed TCP connection or UDP flow.
type Flow struct {
	Protocol            string
	Direction           Direction
	Source              string
	OriginalDestination string
	StartTime           time.Time
	EndTime             time.Time
	Result              string
	Error               string
	BytesSent           int64
	BytesReceived       int64
}

// remapDNSDestination keeps the recorded destination private while sending
// both UDP and TCP DNS requests to the real host resolver.
func remapDNSDestination(original, hostDNS string) string {
	if original == privateDNSDestination {
		return hostDNS
	}
	return original
}
