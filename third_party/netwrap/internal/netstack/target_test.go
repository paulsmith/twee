package netstack

import "testing"

func TestRemapDNSDestination(t *testing.T) {
	tests := []struct {
		name     string
		original string
		want     string
	}{
		{name: "DNS UDP or TCP", original: "10.0.2.3:53", want: "127.0.0.1:15353"},
		{name: "other port", original: "10.0.2.3:80", want: "10.0.2.3:80"},
		{name: "other address", original: "1.1.1.1:53", want: "1.1.1.1:53"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := remapDNSDestination(test.original, "127.0.0.1:15353"); got != test.want {
				t.Fatalf("remapDNSDestination() = %q; want %q", got, test.want)
			}
		})
	}
}
