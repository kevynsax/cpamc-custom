package registry

// CUSTOM (cpamc-custom): hard-coded Claude models that must not depend on remote
// models.json updates. Drop entries once upstream's catalog includes the same id.

func customClaudeHaiku55ModelInfo() *ModelInfo {
	webSearch := true
	return &ModelInfo{
		ID:                  "claude-haiku-5-5",
		Object:              "model",
		Created:             1791331200,
		OwnedBy:             "anthropic",
		Type:                "claude",
		DisplayName:         "Claude Haiku 5.5",
		Description:         "Anthropic's fast Haiku model for high-volume coding and agentic workloads",
		SupportsWebSearch:   true,
		NativeCapabilities:  &NativeCapabilities{WebSearch: &webSearch},
		ContextLength:       1000000,
		MaxCompletionTokens: 128000,
		Thinking: &ThinkingSupport{
			ZeroAllowed:    true,
			DynamicAllowed: true,
			Levels:         []string{"low", "medium", "high", "xhigh", "max"},
		},
		SupportedInputModalities:  []string{"text", "image"},
		SupportedOutputModalities: []string{"text"},
	}
}

// WithClaudeBuiltins injects hard-coded Claude model definitions that should not
// depend on remote models.json updates. Built-ins replace any matching IDs
// already present in the provided slice.
func WithClaudeBuiltins(models []*ModelInfo) []*ModelInfo {
	return upsertModelInfos(models, customClaudeHaiku55ModelInfo())
}
