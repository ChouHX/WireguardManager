package services

import "testing"

func TestClientEndpoint(t *testing.T) {
	for _, tc := range []struct {
		name, override, host string
		port                 int
		want                 string
	}{
		{"tenant port", "", "203.0.113.10", 51821, "203.0.113.10:51821"},
		{"DNS relay", "", " WG.Example.com ", 51820, "wg.example.com:51820"},
		{"explicit forwarded port", "relay.example:62000", "", 51820, "relay.example:62000"},
		{"repair legacy missing host", ":62000", "203.0.113.10", 51820, "203.0.113.10:62000"},
		{"IPv6 host", "", "2001:db8::1", 51820, "[2001:db8::1]:51820"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ClientEndpoint(tc.override, tc.host, tc.port)
			if err != nil || got != tc.want {
				t.Fatalf("got %q, %v; want %q", got, err, tc.want)
			}
		})
	}
}

func TestClientEndpointRejectsMissingOrInvalidAddress(t *testing.T) {
	for _, host := range []string{"", "  ", "https://relay.example", "relay.example:51820", "relay.example/path", "0.0.0.0", "::", "224.0.0.1", "bad domain", ".example.com", "a..example.com"} {
		if _, err := ClientEndpoint("", host, 51820); err == nil {
			t.Errorf("accepted invalid host %q", host)
		}
	}
	for _, override := range []string{":51820", "host:0", "host:65536", "host:abc", "https://host:51820"} {
		if _, err := ClientEndpoint(override, "", 51820); err == nil {
			t.Errorf("accepted invalid override %q", override)
		}
	}
}
