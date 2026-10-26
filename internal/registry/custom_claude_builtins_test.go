package registry

import "testing"

func TestWithClaudeBuiltins_IncludesMythos51(t *testing.T) {
	models := GetClaudeModels()

	var mythos, fable, haiku *ModelInfo
	mythosCount := 0
	for _, model := range models {
		switch model.ID {
		case "claude-mythos-5-1":
			mythos = model
			mythosCount++
		case "claude-fable-5-1":
			fable = model
		case "claude-haiku-5-5":
			haiku = model
		}
	}

	if mythosCount != 1 {
		t.Fatalf("claude-mythos-5-1 count = %d, want 1", mythosCount)
	}
	if haiku == nil {
		t.Fatal("claude-haiku-5-5 missing from the upstream catalog")
	}
	if fable == nil {
		t.Fatal("claude-fable-5-1 missing from the catalog the builtin mirrors")
	}
	if mythos.Type != "claude" || mythos.OwnedBy != "anthropic" {
		t.Fatalf("mythos identity = type %q owned_by %q", mythos.Type, mythos.OwnedBy)
	}
	if mythos.ContextLength != fable.ContextLength || mythos.MaxCompletionTokens != fable.MaxCompletionTokens {
		t.Fatalf("mythos limits = %d/%d, fable = %d/%d",
			mythos.ContextLength, mythos.MaxCompletionTokens, fable.ContextLength, fable.MaxCompletionTokens)
	}
	if !sameStrings(mythos.SupportedInputModalities, fable.SupportedInputModalities) ||
		!sameStrings(mythos.SupportedOutputModalities, fable.SupportedOutputModalities) {
		t.Fatalf("mythos modalities differ from fable")
	}
	if mythos.Thinking == nil || fable.Thinking == nil || !sameStrings(mythos.Thinking.Levels, fable.Thinking.Levels) {
		t.Fatalf("mythos thinking levels differ from fable")
	}

	// A remote catalog that later ships the same id must not produce a duplicate.
	again := WithClaudeBuiltins(models)
	count := 0
	for _, model := range again {
		if model.ID == "claude-mythos-5-1" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("re-applying builtins duplicated claude-mythos-5-1: count = %d", count)
	}
}

func sameStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
