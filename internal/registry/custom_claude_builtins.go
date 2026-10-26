package registry

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

func WithClaudeBuiltins(models []*ModelInfo) []*ModelInfo {
	return upsertModelInfos(models, customClaudeMythos51ModelInfo())
}
