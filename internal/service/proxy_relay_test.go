package service

import "testing"

func TestSupervisorRelayEnabledForRemoteMTLSControlWithoutSSHSocket(t *testing.T) {
	tests := []struct {
		name, sshSocket, controlAddress, targetEndpoint, gatewayURL, token string
		want                                                               bool
	}{
		{name: "remote mTLS control", controlAddress: "supervisor.default.svc:5502", gatewayURL: "https://gateway.test", token: "sandbox-token", want: true},
		{name: "remote target bridge", targetEndpoint: "workload.default.svc:5503", gatewayURL: "https://gateway.test", token: "sandbox-token", want: true},
		{name: "local SSH socket", sshSocket: "/run/ssh.sock", gatewayURL: "https://gateway.test", token: "sandbox-token", want: true},
		{name: "no relay transport", gatewayURL: "https://gateway.test", token: "sandbox-token"},
		{name: "missing gateway", controlAddress: "supervisor.default.svc:5502", token: "sandbox-token"},
		{name: "missing sandbox token", controlAddress: "supervisor.default.svc:5502", gatewayURL: "https://gateway.test"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := supervisorRelayEnabled(tt.sshSocket, tt.controlAddress, tt.targetEndpoint, tt.gatewayURL, tt.token); got != tt.want {
				t.Fatalf("supervisorRelayEnabled() = %t, want %t", got, tt.want)
			}
		})
	}
}

func TestSupervisorControlListenAddressSeparatesServerBindFromWorkloadEndpoint(t *testing.T) {
	if got := supervisorControlListenAddress("0.0.0.0:5502", "10.96.0.13:5502"); got != "0.0.0.0:5502" {
		t.Fatalf("listen address = %q; want explicit server bind", got)
	}
	if got := supervisorControlListenAddress("  ", "127.0.0.1:5502"); got != "127.0.0.1:5502" {
		t.Fatalf("listen address = %q; want legacy endpoint fallback", got)
	}
}
