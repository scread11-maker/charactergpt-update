package main

type ChatUI interface {
	Open(inputUIConfig)
	SetProcessing(requestID string, checkAfterMS int64)
	SetCheckable()
	RearmCheck(checkAfterMS int64)
	SetIdle()
	SetError(string)
	Close()
}

// cjkUIFontCandidates returns preferred UI fonts in order. The Runtime Win32
// frontend uses an explicit CJK-capable font instead of inheriting the locale-
// dependent dialog font, which can render Traditional Chinese as tofu on a
// Japanese Windows installation.
func cjkUIFontCandidates() []string {
	return []string{
		"Microsoft JhengHei UI",
		"Microsoft JhengHei",
		"Microsoft YaHei UI",
		"Microsoft YaHei",
		"Yu Gothic UI",
		"Meiryo UI",
		"Segoe UI",
	}
}

const (
	inputBaseHeight  = 45
	windowBaseHeight = 210
	inputBaseLines   = 2
	inputMaxLines    = 8
	inputLineStepPX  = 24
)

// inputHeightForLineCount grows the chat editor with visual lines, then caps
// the control so long drafts scroll internally instead of growing forever.
func inputHeightForLineCount(lines int) int {
	if lines < 1 {
		lines = 1
	}
	if lines <= inputBaseLines {
		return inputBaseHeight
	}
	if lines > inputMaxLines {
		lines = inputMaxLines
	}
	return inputBaseHeight + (lines-inputBaseLines)*inputLineStepPX
}
