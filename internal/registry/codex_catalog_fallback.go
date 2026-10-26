package registry

func codexBuiltinTerra61ModelInfo() *ModelInfo {
	webSearch := true
	return &ModelInfo{
		ID:                         "gpt-6.1-terra",
		Object:                     "model",
		OwnedBy:                    "openai",
		Type:                       "openai",
		DisplayName:                "GPT 6.1 Terra",
		Version:                    "gpt-6.1-terra",
		Description:                "GPT-6.1 Terra Codex model.",
		ContextLength:              500000,
		MaxContextLength:           872000,
		MaxCompletionTokens:        128000,
		SupportedParameters:        []string{"tools"},
		SupportedInputModalities:   []string{"text", "image"},
		SupportedOutputModalities:  []string{"text"},
		SupportConfigurationUpdate: true,
		NativeCapabilities:         &NativeCapabilities{WebSearch: &webSearch},
		Thinking: &ThinkingSupport{
			Levels: []string{"low", "medium", "high", "xhigh", "max"},
		},
	}
}
