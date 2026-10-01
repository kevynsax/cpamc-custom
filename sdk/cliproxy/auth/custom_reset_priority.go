package auth

// CUSTOM (cpamc-custom): earliest-reset-first priority overlay.
// See CUSTOM_FEATURES.md. The management quota collector computes, per provider,
// which credential resets soonest and publishes a small priority boost here.
// authPriority() adds the boost, so the highest-priority tier (the soonest-reset
// credential) receives traffic until it becomes unavailable (quota/cooldown),
// then the built-in failover moves to the next one.

import "sync"

var customResetBoosts struct {
	mu     sync.RWMutex
	byAuth map[string]int
}

// SetCustomResetPriorityBoosts replaces the whole boost table (authID -> boost).
func SetCustomResetPriorityBoosts(boosts map[string]int) {
	next := make(map[string]int, len(boosts))
	for id, boost := range boosts {
		if id != "" && boost != 0 {
			next[id] = boost
		}
	}
	customResetBoosts.mu.Lock()
	customResetBoosts.byAuth = next
	customResetBoosts.mu.Unlock()
}

// CustomResetPriorityBoosts returns a copy of the current boost table.
func CustomResetPriorityBoosts() map[string]int {
	customResetBoosts.mu.RLock()
	defer customResetBoosts.mu.RUnlock()
	out := make(map[string]int, len(customResetBoosts.byAuth))
	for id, boost := range customResetBoosts.byAuth {
		out[id] = boost
	}
	return out
}

func customResetPriorityBoost(auth *Auth) int {
	if auth == nil || auth.ID == "" {
		return 0
	}
	customResetBoosts.mu.RLock()
	defer customResetBoosts.mu.RUnlock()
	return customResetBoosts.byAuth[auth.ID]
}
