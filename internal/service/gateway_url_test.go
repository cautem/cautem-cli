package service_test

import (
	"testing"

	"github.com/cautem/quarshell-cli/internal/service"
)

func TestGuestGatewayURL(t *testing.T) {
	cases := map[string]string{
		"http://[::1]:7443": "http://host.cautem.internal:7443",
		"https://localhost.example/path/localhost?host=127.0.0.1": "https://localhost.example/path/localhost?host=127.0.0.1",
		"https://remote.example/localhost":                        "https://remote.example/localhost",
		"http://localhost:7443/localhost?host=127.0.0.1":          "http://host.cautem.internal:7443/localhost?host=127.0.0.1",
		"":                                 "",
		"http://127.0.0.1:7443":            "http://host.cautem.internal:7443",
		"http://localhost:7443":            "http://host.cautem.internal:7443",
		"http://host.cautem.internal:7443": "http://host.cautem.internal:7443",
		"http://host.docker.internal:7443": "http://host.cautem.internal:7443",
	}
	for in, want := range cases {
		if got := service.GuestGatewayURL(in); got != want {
			t.Fatalf("GuestGatewayURL(%q)=%q want %q", in, got, want)
		}
	}
}
