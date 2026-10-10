package main

import "testing"

func TestNetworkListenerRequiresExplicitOptIn(t *testing.T) {
	for _, tt := range []struct {
		addr         string
		allow, valid bool
	}{
		{"127.0.0.1:8080", false, true}, {"[::1]:8080", false, true},
		{"0.0.0.0:8080", false, false}, {"100.64.1.2:8080", false, false},
		{"0.0.0.0:8080", true, true}, {"100.64.1.2:8080", true, true},
		{"[::]:8080", true, true}, {"localhost:8080", true, false}, {":8080", true, false}, {"127.0.0.1", true, false},
	} {
		t.Run(tt.addr, func(t *testing.T) {
			err := validateListen(tt.addr, tt.allow)
			if (err == nil) != tt.valid {
				t.Fatalf("allow=%v: %v", tt.allow, err)
			}
		})
	}
}
