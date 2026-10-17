package management

import (
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
	"testing"
)

func TestUsageAccountIdentityIsReadOnlyMetadata(t *testing.T) {
	account := &coreauth.Auth{ID: "usage-identity-only", Metadata: map[string]any{"email": "person@example.test", "access_token": "not-returned", "monthly_fee_usd": 200.0}}
	h := &Handler{}
	email, fee := h.clientUsageAccountInfo(account)
	if email != "person@example.test" || fee == nil || *fee != 200 {
		t.Fatalf("email=%q fee=%v", email, fee)
	}
	if _, present := account.Metadata["access_token"]; !present {
		t.Fatal("metadata mutated")
	}
	account.Metadata = map[string]any{"monthly_fee_usd": -1.0}
	account.Attributes = map[string]string{"email": "second@example.test"}
	email, fee = h.clientUsageAccountInfo(account)
	if email != "second@example.test" || fee != nil {
		t.Fatalf("email=%q invalid fee=%v", email, fee)
	}
	if usageEmail("not an address") != "" {
		t.Fatal("invalid email accepted")
	}
}
