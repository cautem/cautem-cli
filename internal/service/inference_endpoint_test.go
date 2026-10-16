package service

import (
	"context"
	"testing"

	"github.com/cautem/quarshell-core/defaults"
	"github.com/cautem/quarshell-sdk/go/cautem"
)

type providerEndpointStub struct{ record cautem.ProviderRecord }

func (s providerEndpointStub) GetProvider(context.Context, string) (cautem.ProviderRecord, error) {
	return s.record, nil
}
func TestInferenceUpstreamConfiguration(t *testing.T) {
	cases := []struct {
		name   string
		record cautem.ProviderRecord
		want   string
	}{
		{"deepinfra", cautem.ProviderRecord{Type: "deepinfra"}, defaults.InferenceDeepInfra},
		{"unknown", cautem.ProviderRecord{Type: "custom"}, ""},
		{"override", cautem.ProviderRecord{Type: "custom", Config: cautem.ProviderConfig{"base_url": "https://models.example/v1"}}, "https://models.example/v1"},
		{"invalid", cautem.ProviderRecord{Type: "openai", Config: cautem.ProviderConfig{"base_url": "file:///credentials"}}, ""},
		{"userinfo", cautem.ProviderRecord{Type: "openai", Config: cautem.ProviderConfig{"base_url": "https://secret@models.example"}}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := inferenceUpstreamForType("test", providerEndpointStub{tc.record}, context.Background())
			if got != tc.want {
				t.Fatalf("got %q want %q", got, tc.want)
			}
		})
	}
}
