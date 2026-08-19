//go:build windows

package main

import (
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"unsafe"
)

const (
	WM_CREATE           = 0x0001
	WM_DESTROY          = 0x0002
	WM_COMMAND          = 0x0111
	WM_CLOSE            = 0x0010
	WM_SETFONT          = 0x0030
	WM_APP              = 0x8000
	WS_OVERLAPPEDWINDOW = 0x00CF0000
	WS_VISIBLE          = 0x10000000
	WS_CHILD            = 0x40000000
	WS_TABSTOP          = 0x00010000
	WS_VSCROLL          = 0x00200000
	ES_MULTILINE        = 0x0004
	ES_AUTOVSCROLL      = 0x0040
	ES_WANTRETURN       = 0x1000
	CBS_DROPDOWNLIST    = 0x0003
	BS_PUSHBUTTON       = 0x00000000
	CW_USEDEFAULT       = 0x80000000
	SW_SHOW             = 5
	SW_RESTORE          = 9
	CB_ADDSTRING        = 0x0143
	CB_SETCURSEL        = 0x014E
	CB_GETCURSEL        = 0x0147
	CB_GETLBTEXT        = 0x0148
	CB_RESETCONTENT     = 0x014B
	BN_CLICKED          = 0
	EN_CHANGE           = 0x0300
	EM_GETLINECOUNT     = 0x00BA
	IDC_EDIT            = 1001
	IDC_EMOTION         = 1002
	IDC_CHECK           = 1003
	IDC_BUTTON          = 1004
	IDC_STATUS          = 1005
	MSG_PROCESSING      = WM_APP + 1
	MSG_IDLE            = WM_APP + 2
	MSG_ENABLE_CHECK    = WM_APP + 3
	MSG_REARM           = WM_APP + 4
	MSG_ERROR           = WM_APP + 5
	WM_KEYDOWN          = 0x0100
	VK_RETURN           = 0x0D
	VK_SHIFT            = 0x10
	DEFAULT_CHARSET     = 1
	FW_NORMAL           = 400
	CLEARTYPE_QUALITY   = 5
	SWP_NOMOVE          = 0x0002
	SWP_NOZORDER        = 0x0004
	SWP_NOACTIVATE      = 0x0010
)

var (
	user32                = syscall.NewLazyDLL("user32.dll")
	kernel32              = syscall.NewLazyDLL("kernel32.dll")
	gdi32                 = syscall.NewLazyDLL("gdi32.dll")
	pRegisterClassExW     = user32.NewProc("RegisterClassExW")
	pCreateWindowExW      = user32.NewProc("CreateWindowExW")
	pDefWindowProcW       = user32.NewProc("DefWindowProcW")
	pShowWindow           = user32.NewProc("ShowWindow")
	pUpdateWindow         = user32.NewProc("UpdateWindow")
	pGetMessageW          = user32.NewProc("GetMessageW")
	pTranslateMessage     = user32.NewProc("TranslateMessage")
	pDispatchMessageW     = user32.NewProc("DispatchMessageW")
	pPostMessageW         = user32.NewProc("PostMessageW")
	pSendMessageW         = user32.NewProc("SendMessageW")
	pSetWindowTextW       = user32.NewProc("SetWindowTextW")
	pGetWindowTextLengthW = user32.NewProc("GetWindowTextLengthW")
	pGetWindowTextW       = user32.NewProc("GetWindowTextW")
	pEnableWindow         = user32.NewProc("EnableWindow")
	pSetForegroundWindow  = user32.NewProc("SetForegroundWindow")
	pSetWindowPos         = user32.NewProc("SetWindowPos")
	pGetKeyState          = user32.NewProc("GetKeyState")
	pDestroyWindow        = user32.NewProc("DestroyWindow")
	pPostQuitMessage      = user32.NewProc("PostQuitMessage")
	pGetModuleHandleW     = kernel32.NewProc("GetModuleHandleW")
	pCreateFontW          = gdi32.NewProc("CreateFontW")
	pDeleteObject         = gdi32.NewProc("DeleteObject")
	pSelectObject         = gdi32.NewProc("SelectObject")
	pGetTextFaceW         = gdi32.NewProc("GetTextFaceW")
	pGetDC                = user32.NewProc("GetDC")
	pReleaseDC            = user32.NewProc("ReleaseDC")
)

type WNDCLASSEX struct {
	CbSize     uint32
	Style      uint32
	WndProc    uintptr
	ClsExtra   int32
	WndExtra   int32
	Instance   syscall.Handle
	Icon       syscall.Handle
	Cursor     syscall.Handle
	Background syscall.Handle
	MenuName   *uint16
	ClassName  *uint16
	IconSm     syscall.Handle
}
type MSG struct {
	Hwnd    syscall.Handle
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pt      struct{ X, Y int32 }
}

type winUI struct {
	mu                                              sync.Mutex
	hwnd, edit, emotion, checkCombo, button, status syscall.Handle
	submit                                          func(string, string, int64)
	checkFn                                         func()
	logf                                            func(string, ...any)
	cfg                                             inputUIConfig
	font                                            syscall.Handle
	fontName                                        string
	processing                                      bool
	inputHeight                                     int
	requestID                                       string
	errorText                                       string
}

var activeUI *winUI

func NewChatUI(s func(string, string, int64), c func(), l func(string, ...any)) ChatUI {
	u := &winUI{submit: s, checkFn: c, logf: l}
	activeUI = u
	go u.loop()
	return u
}
func u16(s string) *uint16 { p, _ := syscall.UTF16PtrFromString(s); return p }
func (u *winUI) loop() {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	class := u16("SSPGPT062ChatWindow")
	inst, _, _ := pGetModuleHandleW.Call(0)
	wc := WNDCLASSEX{CbSize: uint32(unsafe.Sizeof(WNDCLASSEX{})), WndProc: syscall.NewCallback(wndProc), Instance: syscall.Handle(inst), Background: syscall.Handle(6), ClassName: class}
	pRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))
	hw, _, _ := pCreateWindowExW.Call(0, uintptr(unsafe.Pointer(class)), uintptr(unsafe.Pointer(u16("想說的話"))), WS_OVERLAPPEDWINDOW, uintptr(int32(200)), uintptr(int32(200)), uintptr(int32(650)), uintptr(int32(210)), 0, 0, inst, 0)
	u.mu.Lock()
	u.hwnd = syscall.Handle(hw)
	u.mu.Unlock()
	u.font, u.fontName = createPreferredUIFont()
	if u.logf != nil {
		u.logf("UI_FONT selected=%q explicit=%t", u.fontName, u.font != 0)
	}
	u.createControls(syscall.Handle(hw), syscall.Handle(inst))
	var m MSG
	for {
		r, _, _ := pGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(r) <= 0 {
			return
		}
		if m.Message == WM_KEYDOWN && m.Hwnd == u.edit && m.WParam == VK_RETURN && !keyDown(VK_SHIFT) {
			u.onButton()
			continue
		}
		pTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		pDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
	}
}
func (u *winUI) createControls(hw, inst syscall.Handle) {
	mk := func(cls, text string, style uintptr, x, y, w, h int, id int) syscall.Handle {
		v, _, _ := pCreateWindowExW.Call(0, uintptr(unsafe.Pointer(u16(cls))), uintptr(unsafe.Pointer(u16(text))), WS_CHILD|WS_VISIBLE|style, uintptr(x), uintptr(y), uintptr(w), uintptr(h), uintptr(hw), uintptr(id), uintptr(inst), 0)
		hctl := syscall.Handle(v)
		if u.font != 0 && hctl != 0 {
			pSendMessageW.Call(uintptr(hctl), WM_SETFONT, uintptr(u.font), 1)
		}
		return hctl
	}
	mk("STATIC", "情緒", 0, 12, 15, 45, 22, 0)
	u.emotion = mk("COMBOBOX", "", CBS_DROPDOWNLIST|WS_TABSTOP|WS_VSCROLL, 58, 12, 130, 260, IDC_EMOTION)
	mk("STATIC", "檢查間隔", 0, 200, 15, 78, 22, 0)
	u.checkCombo = mk("COMBOBOX", "", CBS_DROPDOWNLIST|WS_TABSTOP|WS_VSCROLL, 280, 12, 105, 220, IDC_CHECK)
	u.status = mk("STATIC", "就緒", 0, 400, 15, 215, 22, IDC_STATUS)
	u.edit = mk("EDIT", "", ES_MULTILINE|ES_AUTOVSCROLL|ES_WANTRETURN|WS_VSCROLL|WS_TABSTOP|0x00800000, 12, 48, 505, inputBaseHeight, IDC_EDIT)
	u.button = mk("BUTTON", "送出", BS_PUSHBUTTON|WS_TABSTOP, 530, 48, 85, 45, IDC_BUTTON)
	u.inputHeight = inputBaseHeight
}

func keyDown(vk uintptr) bool {
	r, _, _ := pGetKeyState.Call(vk)
	return r&0x8000 != 0
}

func (u *winUI) updateInputHeight() {
	u.mu.Lock()
	edit := u.edit
	hwnd := u.hwnd
	oldHeight := u.inputHeight
	u.mu.Unlock()
	if edit == 0 || hwnd == 0 {
		return
	}
	lineCount, _, _ := pSendMessageW.Call(uintptr(edit), EM_GETLINECOUNT, 0, 0)
	newHeight := inputHeightForLineCount(int(lineCount))
	if oldHeight == 0 {
		oldHeight = inputBaseHeight
	}
	if newHeight == oldHeight {
		return
	}
	pSetWindowPos.Call(uintptr(edit), 0, 12, 48, 505, uintptr(newHeight), SWP_NOZORDER|SWP_NOACTIVATE)
	windowHeight := windowBaseHeight + (newHeight - inputBaseHeight)
	pSetWindowPos.Call(uintptr(hwnd), 0, 0, 0, 650, uintptr(windowHeight), SWP_NOMOVE|SWP_NOZORDER|SWP_NOACTIVATE)
	u.mu.Lock()
	u.inputHeight = newHeight
	u.mu.Unlock()
}

func wndProc(hwnd uintptr, msg uint32, wparam, lparam uintptr) uintptr {
	u := activeUI
	if u == nil {
		r, _, _ := pDefWindowProcW.Call(hwnd, uintptr(msg), wparam, lparam)
		return r
	}
	switch msg {
	case WM_COMMAND:
		id := int(wparam & 0xffff)
		code := int((wparam >> 16) & 0xffff)
		if id == IDC_BUTTON && code == BN_CLICKED {
			u.onButton()
			return 0
		}
		if id == IDC_EDIT && code == EN_CHANGE {
			u.updateInputHeight()
			return 0
		}
	case WM_CLOSE:
		pShowWindow.Call(hwnd, 0)
		return 0
	case WM_DESTROY:
		u.releaseUIFont()
		pPostQuitMessage.Call(0)
		return 0
	case MSG_PROCESSING:
		u.applyProcessing()
		return 0
	case MSG_IDLE:
		u.applyIdle()
		return 0
	case MSG_ENABLE_CHECK:
		pEnableWindow.Call(uintptr(u.button), 1)
		setText(u.status, "仍在思考，可按「檢查」")
		return 0
	case MSG_REARM:
		u.applyRearm()
		return 0
	case MSG_ERROR:
		u.applyError()
		return 0
	}
	r, _, _ := pDefWindowProcW.Call(hwnd, uintptr(msg), wparam, lparam)
	return r
}
func (u *winUI) Open(c inputUIConfig) {
	u.mu.Lock()
	u.cfg = c
	hw := u.hwnd
	u.mu.Unlock()
	if hw == 0 {
		return
	}
	u.fillCombos()
	pShowWindow.Call(uintptr(hw), SW_RESTORE)
	pShowWindow.Call(uintptr(hw), SW_SHOW)
	pSetForegroundWindow.Call(uintptr(hw))
}
func (u *winUI) fillCombos() {
	u.mu.Lock()
	cfg := u.cfg
	em := u.emotion
	cc := u.checkCombo
	u.mu.Unlock()
	pSendMessageW.Call(uintptr(em), CB_RESETCONTENT, 0, 0)
	pSendMessageW.Call(uintptr(cc), CB_RESETCONTENT, 0, 0)
	for _, x := range cfg.Emotions {
		pSendMessageW.Call(uintptr(em), CB_ADDSTRING, 0, uintptr(unsafe.Pointer(u16(x))))
	}
	idx := 0
	for i, x := range cfg.Emotions {
		if x == cfg.DefaultEmotion {
			idx = i
		}
	}
	pSendMessageW.Call(uintptr(em), CB_SETCURSEL, uintptr(idx), 0)
	for _, x := range cfg.CheckThresholds {
		pSendMessageW.Call(uintptr(cc), CB_ADDSTRING, 0, uintptr(unsafe.Pointer(u16(x.Label))))
	}
	ci := 0
	for i, x := range cfg.CheckThresholds {
		if x.Milliseconds == cfg.DefaultCheckMS {
			ci = i
		}
	}
	pSendMessageW.Call(uintptr(cc), CB_SETCURSEL, uintptr(ci), 0)
}
func (u *winUI) onButton() {
	u.mu.Lock()
	processing := u.processing
	u.mu.Unlock()
	if processing {
		u.checkFn()
		return
	}
	text := getText(u.edit)
	if text == "" {
		return
	}
	ei, _, _ := pSendMessageW.Call(uintptr(u.emotion), CB_GETCURSEL, 0, 0)
	ci, _, _ := pSendMessageW.Call(uintptr(u.checkCombo), CB_GETCURSEL, 0, 0)
	u.mu.Lock()
	cfg := u.cfg
	u.mu.Unlock()
	emotion := "neutral"
	if int(ei) >= 0 && int(ei) < len(cfg.Emotions) {
		emotion = cfg.Emotions[int(ei)]
	}
	ms := int64(0)
	if int(ci) >= 0 && int(ci) < len(cfg.CheckThresholds) {
		ms = cfg.CheckThresholds[int(ci)].Milliseconds
	}
	u.submit(text, emotion, ms)
}
func (u *winUI) SetProcessing(id string, ms int64) {
	u.mu.Lock()
	u.processing = true
	u.requestID = id
	u.mu.Unlock()
	pPostMessageW.Call(uintptr(u.hwnd), MSG_PROCESSING, 0, 0)
}
func (u *winUI) SetCheckable() {
	pPostMessageW.Call(uintptr(u.hwnd), MSG_ENABLE_CHECK, 0, 0)
}
func (u *winUI) applyProcessing() {
	setText(u.button, "檢查")
	setText(u.status, "思考中…")
	pEnableWindow.Call(uintptr(u.edit), 0)
	pEnableWindow.Call(uintptr(u.emotion), 0)
	pEnableWindow.Call(uintptr(u.checkCombo), 0)
	pEnableWindow.Call(uintptr(u.button), 0)
}
func (u *winUI) RearmCheck(ms int64) {
	pPostMessageW.Call(uintptr(u.hwnd), MSG_REARM, 0, 0)
}
func (u *winUI) applyRearm() {
	pEnableWindow.Call(uintptr(u.button), 0)
	setText(u.status, "繼續等待…")
}
func (u *winUI) SetIdle() {
	u.mu.Lock()
	u.processing = false
	u.requestID = ""
	u.mu.Unlock()
	pPostMessageW.Call(uintptr(u.hwnd), MSG_IDLE, 0, 0)
}
func (u *winUI) applyIdle() {
	setText(u.button, "送出")
	setText(u.status, "就緒")
	setText(u.edit, "")
	pEnableWindow.Call(uintptr(u.edit), 1)
	pEnableWindow.Call(uintptr(u.emotion), 1)
	pEnableWindow.Call(uintptr(u.checkCombo), 1)
	pEnableWindow.Call(uintptr(u.button), 1)
}

func (u *winUI) SetError(msg string) {
	u.mu.Lock()
	u.processing = false
	u.requestID = ""
	u.errorText = msg
	u.mu.Unlock()
	pPostMessageW.Call(uintptr(u.hwnd), MSG_ERROR, 0, 0)
}
func (u *winUI) applyError() {
	u.mu.Lock()
	msg := u.errorText
	u.mu.Unlock()
	setText(u.button, "送出")
	setText(u.status, "失敗："+msg)
	pEnableWindow.Call(uintptr(u.edit), 1)
	pEnableWindow.Call(uintptr(u.emotion), 1)
	pEnableWindow.Call(uintptr(u.checkCombo), 1)
	pEnableWindow.Call(uintptr(u.button), 1)
}

func createUIFont(face string) syscall.Handle {
	// Negative height requests a character height rather than a cell height.
	// 18 logical pixels closely matches the existing dialog sizing while
	// leaving the glyph repertoire to the selected CJK font.
	h, _, _ := pCreateFontW.Call(
		uintptr(^uint32(17)), // -18 as signed int32
		0, 0, 0,
		FW_NORMAL,
		0, 0, 0,
		DEFAULT_CHARSET,
		0, 0,
		CLEARTYPE_QUALITY,
		0,
		uintptr(unsafe.Pointer(u16(face))),
	)
	return syscall.Handle(h)
}

func selectedFontFace(hfont syscall.Handle) string {
	if hfont == 0 {
		return ""
	}
	dc, _, _ := pGetDC.Call(0)
	if dc == 0 {
		return ""
	}
	defer pReleaseDC.Call(0, dc)
	old, _, _ := pSelectObject.Call(dc, uintptr(hfont))
	if old == 0 {
		return ""
	}
	defer pSelectObject.Call(dc, old)
	buf := make([]uint16, 128)
	n, _, _ := pGetTextFaceW.Call(dc, uintptr(len(buf)), uintptr(unsafe.Pointer(&buf[0])))
	if n == 0 {
		return ""
	}
	return syscall.UTF16ToString(buf)
}

func createPreferredUIFont() (syscall.Handle, string) {
	for _, face := range cjkUIFontCandidates() {
		h := createUIFont(face)
		if h == 0 {
			continue
		}
		actual := selectedFontFace(h)
		if strings.EqualFold(actual, face) {
			return h, actual
		}
		pDeleteObject.Call(uintptr(h))
	}
	return 0, "system-default"
}

func (u *winUI) releaseUIFont() {
	u.mu.Lock()
	h := u.font
	u.font = 0
	u.mu.Unlock()
	if h != 0 {
		pDeleteObject.Call(uintptr(h))
	}
}

func (u *winUI) Close() {
	u.mu.Lock()
	hw := u.hwnd
	u.mu.Unlock()
	if hw != 0 {
		pDestroyWindow.Call(uintptr(hw))
	}
}
func setText(h syscall.Handle, s string) {
	pSetWindowTextW.Call(uintptr(h), uintptr(unsafe.Pointer(u16(s))))
}
func getText(h syscall.Handle) string {
	n, _, _ := pGetWindowTextLengthW.Call(uintptr(h))
	if n == 0 {
		return ""
	}
	buf := make([]uint16, n+1)
	pGetWindowTextW.Call(uintptr(h), uintptr(unsafe.Pointer(&buf[0])), n+1)
	return syscall.UTF16ToString(buf)
}

var _ = strconv.Itoa
