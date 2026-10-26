//go:build windows

// 图标与系统托盘：左键打开设置，右键菜单（设置 / 退出），悬停显示连续使用时长。
package main

import (
	"fmt"
	"syscall"
	"time"
	"unicode/utf16"
	"unsafe"
)

var (
	shell32 = syscall.NewLazyDLL("shell32.dll")

	pShellNotifyIconW       = shell32.NewProc("Shell_NotifyIconW")
	pLoadImageW             = user32.NewProc("LoadImageW")
	pGetSystemMetrics       = user32.NewProc("GetSystemMetrics")
	pRegisterWindowMessageW = user32.NewProc("RegisterWindowMessageW")
	pCreatePopupMenu        = user32.NewProc("CreatePopupMenu")
	pAppendMenuW            = user32.NewProc("AppendMenuW")
	pSetMenuDefaultItem     = user32.NewProc("SetMenuDefaultItem")
	pTrackPopupMenu         = user32.NewProc("TrackPopupMenu")
	pDestroyMenu            = user32.NewProc("DestroyMenu")
	pGetCursorPos           = user32.NewProc("GetCursorPos")
)

const (
	wmAppTray    = 0x8002 // WM_APP+2：托盘图标回调
	wmNull       = 0x0000
	wmRButtonUp  = 0x0205
	nimAdd       = 0
	nimModify    = 1
	nimDelete    = 2
	nifMessage   = 0x1
	nifIcon      = 0x2
	nifTip       = 0x4
	imageIcon    = 1
	smCxIcon     = 11
	smCyIcon     = 12
	smCxSmIcon   = 49
	smCySmIcon   = 50
	mfString     = 0x0000
	mfSeparator  = 0x0800
	tpmRightBtn  = 0x0002
	tpmNoNotify  = 0x0080
	tpmReturnCmd = 0x0100
	menuSettings = 1
	menuQuit     = 2
	trayID       = 1
	appIconResID = 1 // winres.json 中 RT_GROUP_ICON "#1"
)

type notifyIconData struct {
	CbSize           uint32
	HWnd             uintptr
	UID              uint32
	UFlags           uint32
	UCallbackMessage uint32
	HIcon            uintptr
	SzTip            [128]uint16
	DwState          uint32
	DwStateMask      uint32
	SzInfo           [256]uint16
	UVersion         uint32
	SzInfoTitle      [64]uint16
	DwInfoFlags      uint32
	GuidItem         [16]byte
	HBalloonIcon     uintptr
}

var (
	hIconSmall, hIconLarge uintptr
	taskbarCreatedMsg      uintptr // Explorer 重启后广播，需重新添加托盘图标
	trayAdded              bool
	trayTip                string
)

func loadIcons() {
	load := func(cxIdx, cyIdx uintptr) uintptr {
		cx, _, _ := pGetSystemMetrics.Call(cxIdx)
		cy, _, _ := pGetSystemMetrics.Call(cyIdx)
		h, _, _ := pLoadImageW.Call(hInstance, appIconResID, imageIcon, cx, cy, 0)
		return h
	}
	hIconSmall = load(smCxSmIcon, smCySmIcon)
	hIconLarge = load(smCxIcon, smCyIcon)
	taskbarCreatedMsg, _, _ = pRegisterWindowMessageW.Call(uintptr(unsafe.Pointer(u16("TaskbarCreated"))))
}

func trayCall(op uintptr, tip string) bool {
	nid := notifyIconData{HWnd: hostHwnd, UID: trayID, UFlags: nifMessage | nifIcon | nifTip,
		UCallbackMessage: wmAppTray, HIcon: hIconSmall}
	nid.CbSize = uint32(unsafe.Sizeof(nid))
	copy(nid.SzTip[:len(nid.SzTip)-1], utf16.Encode([]rune(tip)))
	r, _, _ := pShellNotifyIconW.Call(op, uintptr(unsafe.Pointer(&nid)))
	return r != 0
}

// traySync 让托盘状态与配置一致；开机自启时 Explorer 可能尚未就绪，由轮询重试
func traySync() {
	switch {
	case cfg.Tray && !trayAdded:
		trayAdded = trayCall(nimAdd, trayTipText())
	case !cfg.Tray && trayAdded:
		trayCall(nimDelete, "")
		trayAdded = false
	}
}

func trayUpdateTip() {
	if tip := trayTipText(); trayAdded && tip != trayTip {
		trayTip = tip
		trayCall(nimModify, tip)
	}
}

func trayTipText() string {
	if tracker != nil && tracker.IsPaused() {
		return appTitle + " · " + pauseNote()
	}
	if tracker == nil || tracker.IsAway() {
		return appTitle + " · 休息中"
	}
	return fmt.Sprintf("%s · 已连续使用 %d 分钟", appTitle, int(tracker.ActiveFor(time.Now()).Minutes()))
}

func trayRemove() {
	if trayAdded {
		trayCall(nimDelete, "")
		trayAdded = false
	}
}

func trayMenu() {
	menu, _, _ := pCreatePopupMenu.Call()
	appendPauseItems(menu)
	pAppendMenuW.Call(menu, mfSeparator, 0, 0)
	pAppendMenuW.Call(menu, mfString, menuSettings, uintptr(unsafe.Pointer(u16("设置…"))))
	pAppendMenuW.Call(menu, mfSeparator, 0, 0)
	pAppendMenuW.Call(menu, mfString, menuQuit, uintptr(unsafe.Pointer(u16("退出"))))
	pSetMenuDefaultItem.Call(menu, menuSettings, 0)

	var pt point
	pGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
	pSetForegroundWindow.Call(hostHwnd) // 不置前的话，点菜单外部菜单不会消失（Win32 已知行为）
	cmd, _, _ := pTrackPopupMenu.Call(menu, tpmRightBtn|tpmNoNotify|tpmReturnCmd,
		uintptr(pt.X), uintptr(pt.Y), 0, hostHwnd, 0)
	pPostMessageW.Call(hostHwnd, wmNull, 0, 0)
	pDestroyMenu.Call(menu)

	switch cmd {
	case menuSettings:
		showSettings()
	case menuQuit:
		pDestroyWindow.Call(hostHwnd)
	default:
		handlePauseCmd(cmd)
	}
}

// onTrayEvent 托盘回调：左键打开设置，右键弹菜单
func onTrayEvent(lp uintptr) {
	switch lp & 0xFFFF {
	case wmLButtonUp:
		showSettings()
	case wmRButtonUp:
		trayMenu()
	}
}
