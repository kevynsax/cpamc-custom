package registry

// CUSTOM (cpamc-custom): verification-gated Mythos entries. Anthropic serves
// Mythos only to verified accounts, so the public catalog does not list it.

func customClaudeMythosModelInfo(id, displayName, description string, created int64) *ModelInfo {
	return &ModelInfo{
		ID:                  id,
		Object:              "model",
		Created:             created,
		OwnedBy:             "anthropic",
		Type:                "claude",
		DisplayName:         displayName,
		Description:         description,
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

func WithClaudeBuiltins(models []*ModelInfo) []*ModelInfo {
	mythos51 := customClaudeMythosModelInfo("claude-mythos-5-1", "Claude Mythos 5.1",
		"Claude Fable 5.1 for accounts verified through Anthropic's verification programs. Same specifications: 1M context, 128K output, adaptive thinking.",
		1788220800)
	mythos52 := customClaudeMythosModelInfo("claude-mythos-5-2", "Claude Mythos 5.2",
		"Claude Fable 5.2 for accounts verified through Anthropic's verification programs. Same specifications: 1M context, 128K output, adaptive thinking.",
		1794182400)
	return upsertModelInfos(models, mythos51, mythos52)
}
