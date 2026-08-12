package codegen

import "unicode/utf8"

type markerPromptResult uint8

const (
	markerPromptContinue markerPromptResult = iota
	markerPromptCancel
	markerPromptCommit
)

type markerPrompt struct {
	active bool
	label  string
}

func (p *markerPrompt) start() {
	p.active = true
	p.label = ""
}

func (p *markerPrompt) clear() {
	p.active = false
	p.label = ""
}

func (p *markerPrompt) handle(in inputEvent) markerPromptResult {
	switch in.kind {
	case inputType, inputPaste:
		p.label += in.text
	case inputKey:
		switch in.key {
		case "Escape", "Ctrl+C":
			return markerPromptCancel
		case "Enter":
			if p.label != "" {
				return markerPromptCommit
			}
		case "Backspace":
			if p.label != "" {
				_, size := utf8.DecodeLastRuneInString(p.label)
				p.label = p.label[:len(p.label)-size]
			}
		}
	}
	return markerPromptContinue
}
