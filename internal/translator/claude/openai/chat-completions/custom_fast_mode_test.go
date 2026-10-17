package chat_completions

// CUSTOM (cpamc-custom): chat-completions -> Claude fast mode mapping.

import (
	"testing"

	"github.com/tidwall/gjson"
)

func TestCustomFastModeChatCompletionsMapping(t *testing.T) {
	cases := []struct {
		name string
		body string
		fast bool
	}{
		{"speed fast", `{"model":"claude-opus-5-5","speed":"fast","messages":[{"role":"user","content":"hi"}]}`, true},
		{"service_tier priority", `{"model":"claude-opus-5-5","service_tier":"priority","messages":[{"role":"user","content":"hi"}]}`, true},
		{"default", `{"model":"claude-opus-5-5","messages":[{"role":"user","content":"hi"}]}`, false},
		{"service_tier auto", `{"model":"claude-opus-5-5","service_tier":"auto","messages":[{"role":"user","content":"hi"}]}`, false},
	}
	for _, tc := range cases {
		out, err := ConvertOpenAIRequestToClaude("claude-opus-5-5", []byte(tc.body), true)
		if err != nil {
			t.Fatalf("%s: convert request: %v", tc.name, err)
		}
		got := gjson.GetBytes(out, "speed").String() == "fast"
		if got != tc.fast {
			t.Errorf("%s: speed=fast is %v, want %v (out=%s)", tc.name, got, tc.fast, gjson.GetBytes(out, "speed").Raw)
		}
		if gjson.GetBytes(out, "service_tier").Exists() {
			t.Errorf("%s: service_tier must not leak to Claude", tc.name)
		}
	}
}
