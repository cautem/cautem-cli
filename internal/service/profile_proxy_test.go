package service

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cautem/cautem-core/policy"
	"github.com/cautem/cautem-providers/provider"
	"github.com/cautem/cautem-proxy/proxy"
)

func TestCompatOpenAIProfileComposesAndRewritesMockRequest(t *testing.T) {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		if errors.Is(err, net.ErrClosed) || strings.Contains(strings.ToLower(err.Error()), "operation not permitted") {
			t.Skipf("sandbox does not permit binding a local HTTP test endpoint: %v", err)
		}
		t.Fatal(err)
	}
	var gotAuthorization string
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuthorization = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusNoContent)
	}))
	server.Listener = listener
	server.Start()
	t.Cleanup(server.Close)

	profile, err := provider.ParseYAML([]byte(`id: mock-openai
display_name: Mock OpenAI
inference_capable: true
credentials:
  - name: api_key
    env_vars: [OPENAI_API_KEY]
    required: true
    auth_style: bearer
    header_name: authorization
discovery:
  credentials: [api_key]
endpoints:
  - host: api.openai.com
    port: 443
    protocol: rest
    access: read-write
    enforcement: enforce
    tls: terminate
`))
	if err != nil {
		t.Fatal(err)
	}
	host, portText, err := net.SplitHostPort(listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	var port int
	if _, err := fmt.Sscanf(portText, "%d", &port); err != nil {
		t.Fatal(err)
	}
	profile.Endpoints[0].Host = host
	profile.Endpoints[0].Port = port
	profile.Endpoints[0].TLS = "clear"
	effective, err := provider.EffectivePolicy(policy.Document{Version: 1}, []provider.Layer{{
		InstanceName: "openai", Profile: profile, EnvVars: []string{"OPENAI_API_KEY"},
	}}, false)
	if err != nil {
		t.Fatal(err)
	}
	rules := effective.NetworkAllows()
	if len(rules) != 1 || len(rules[0].CredentialKeys) != 1 || rules[0].CredentialKeys[0] != "OPENAI_API_KEY" {
		t.Fatalf("profile credential binding = %#v", rules)
	}
	secrets := proxy.SecretStore{"OPENAI_API_KEY": "sk-test"}
	req, err := http.NewRequest(http.MethodGet, server.URL+"/v1/models", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer cautem:resolve:env:OPENAI_API_KEY")
	bound, err := proxy.SecretsForEndpoint(secrets, rules[0].CredentialKeys, proxy.PlaceholderKeysInRequest(req))
	if err != nil {
		t.Fatal(err)
	}
	if err := proxy.RewriteHTTPRequest(req, bound); err != nil {
		t.Fatal(err)
	}
	response, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if gotAuthorization != "Bearer sk-test" {
		t.Fatalf("mock endpoint authorization = %q", gotAuthorization)
	}

	wrongEndpoint, _ := http.NewRequest(http.MethodGet, server.URL+"/v1/models", nil)
	wrongEndpoint.Header.Set("Authorization", "Bearer cautem:resolve:env:GH_TOKEN")
	if _, err := proxy.SecretsForEndpoint(secrets, rules[0].CredentialKeys, proxy.PlaceholderKeysInRequest(wrongEndpoint)); !errors.Is(err, proxy.ErrCredentialEndpointMismatch) {
		t.Fatalf("wrong endpoint credential binding error = %v", err)
	}
}
