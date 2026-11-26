package auth

// CUSTOM (cpamc-custom)
func (a *Auth) AgyProxyOnly() bool {
	if a == nil || a.Provider != "antigravity" {
		return false
	}
	proxyOnly, _ := a.Metadata["agy_proxy_only"].(bool)
	return proxyOnly
}
