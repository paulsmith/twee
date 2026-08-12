package codegen

import "testing"

func TestMarkerPrompt(t *testing.T) {
	var prompt markerPrompt
	prompt.start()
	if got := prompt.handle(inputEvent{kind: inputType, text: "ready"}); got != markerPromptContinue || prompt.label != "ready" {
		t.Fatalf("type = %v label %q", got, prompt.label)
	}
	if got := prompt.handle(inputEvent{kind: inputKey, key: "Backspace"}); got != markerPromptContinue || prompt.label != "read" {
		t.Fatalf("backspace = %v label %q", got, prompt.label)
	}
	if got := prompt.handle(inputEvent{kind: inputKey, key: "Enter"}); got != markerPromptCommit {
		t.Fatalf("enter = %v, want commit", got)
	}

	prompt.start()
	if got := prompt.handle(inputEvent{kind: inputKey, key: "Enter"}); got != markerPromptContinue {
		t.Fatalf("empty enter = %v, want continue", got)
	}
	for _, key := range []string{"Escape", "Ctrl+C"} {
		prompt.start()
		if got := prompt.handle(inputEvent{kind: inputKey, key: key}); got != markerPromptCancel {
			t.Fatalf("%s = %v, want cancel", key, got)
		}
	}
}

func TestMarkerPromptBackspaceRemovesRune(t *testing.T) {
	prompt := markerPrompt{active: true, label: "a界"}
	prompt.handle(inputEvent{kind: inputKey, key: "Backspace"})
	if prompt.label != "a" {
		t.Fatalf("label = %q, want a", prompt.label)
	}
}
