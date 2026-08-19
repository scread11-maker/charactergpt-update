//go:build !windows

package main

type stubUI struct {
	submit func(string, string, int64)
	check  func()
	logf   func(string, ...any)
}

func NewChatUI(s func(string, string, int64), c func(), l func(string, ...any)) ChatUI {
	return &stubUI{submit: s, check: c, logf: l}
}
func (u *stubUI) Open(c inputUIConfig) {
	u.logf("UI_OPEN emotions=%v default_check=%d", c.Emotions, c.DefaultCheckMS)
}
func (u *stubUI) SetProcessing(id string, ms int64) {
	u.logf("UI_PROCESSING id=%s check_after=%d", id, ms)
}
func (u *stubUI) SetCheckable()       { u.logf("UI_CHECKABLE") }
func (u *stubUI) RearmCheck(ms int64) { u.logf("UI_REARM check_after=%d", ms) }
func (u *stubUI) SetIdle()            { u.logf("UI_IDLE") }
func (u *stubUI) SetError(s string)   { u.logf("UI_ERROR %s", s) }
func (u *stubUI) Close()              {}
