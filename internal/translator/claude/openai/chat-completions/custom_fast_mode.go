package chat_completions

// CUSTOM (cpamc-custom): Claude fast mode for OpenAI chat-completions clients.
// See CUSTOM_FEATURES.md.
//
// A chat request selects Claude fast mode with any of:
//   - "speed": "fast"            (Anthropic's native field, passed through)
//   - "service_tier": "priority" (OpenAI convention, same mapping as the Responses translator)
//
// The Claude executor then adds the fast-mode-2026-02-01 beta and treats the
// "usage credits are required for fast mode" 429 as request-scoped (no cooldown).

import (
	"strings"

	"github.com/tidwall/gjson"
)

func claudeChatRequestWantsFast(root gjson.Result) bool {
	if speed := root.Get("speed"); speed.Type == gjson.String && strings.EqualFold(strings.TrimSpace(speed.String()), "fast") {
		return true
	}
	tier := root.Get("service_tier")
	return tier.Type == gjson.String && strings.EqualFold(strings.TrimSpace(tier.String()), "priority")
}
