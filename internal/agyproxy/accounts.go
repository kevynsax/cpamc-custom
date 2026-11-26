package agyproxy

// CUSTOM (cpamc-custom)

import (
	"net/http"
	"slices"
	"strings"

	"github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
)

type AccountSummary struct {
	ID     string `json:"id"`
	Email  string `json:"email"`
	Status string `json:"status"`
}

func (p *Proxy) Accounts() []AccountSummary {
	accounts := p.manager.List()
	result := make([]AccountSummary, 0)
	for _, account := range accounts {
		if !account.AgyProxyOnly() {
			continue
		}
		email, _ := account.Metadata["email"].(string)
		email = strings.TrimSpace(email)
		if email == "" {
			continue
		}
		status := "ready"
		_, tokenStatus := accountToken(account)
		if tokenStatus != http.StatusOK {
			status = "refreshing"
		}
		if account.Disabled || account.Status == auth.StatusDisabled {
			status = "disabled"
		}
		entry := AccountSummary{ID: account.ID, Email: email, Status: status}
		result = append(result, entry)
	}
	compare := func(left, right AccountSummary) int {
		return strings.Compare(left.Email, right.Email)
	}
	slices.SortFunc(result, compare)
	return result
}
