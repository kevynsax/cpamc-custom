package registry

// CUSTOM (cpamc-custom): hard-coded Claude models that must not depend on remote
// models.json updates. Drop entries once upstream's catalog includes the same id.
//
// claude-mythos-5-1 stays here on purpose. Upstream refuses to list it publicly
// (CLIProxyAPI #6428): it is Claude Fable 5.1 under a verification-gated id, so
// only credentials whose account actually has Mythos access can serve it.

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

// customClaudeMythos51ModelInfo mirrors the embedded claude-fable-5-1 entry.
// Anthropic documents Mythos 5.1 as the same model (1M context, 128K output,
// adaptive thinking, text+image in, text out); only the id and access gate differ.
func customClaudeMythos51ModelInfo() *ModelInfo {
	return &ModelInfo{
		ID:                  "claude-mythos-5-1",
		Object:              "model",
		Created:             1788220800,
		OwnedBy:             "anthropic",
		Type:                "claude",
		DisplayName:         "Claude Mythos 5.1",
		Description:         "Claude Fable 5.1 for accounts verified through Anthropic's verification programs. Same specifications: 1M context, 128K output, adaptive thinking.",
		ContextLength:       1000000,
		MaxCompletionTokens: 128000,
		Thinking: &ThinkingSupport{
			Min:         1024,
			Max:         128000,
			ZeroAllowed: true,
			Levels:      []string{"low", "medium", "high", "xhigh", "max"},
		},
		SupportedInputModalities:  []string{"text", "image"},
		SupportedOutputModalities: []string{"text"},
	}
}

// WithClaudeBuiltins injects hard-coded Claude model definitions that should not
// depend on remote models.json updates. Built-ins replace any matching IDs
// already present in the provided slice.
func WithClaudeBuiltins(models []*ModelInfo) []*ModelInfo {
	return upsertModelInfos(models, customClaudeHaiku55ModelInfo(), customClaudeMythos51ModelInfo())
}
