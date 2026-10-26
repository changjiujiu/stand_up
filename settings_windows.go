//go:build windows

// 设置窗口：布局与绘制在 ui.go（平台无关），这里只负责窗口、输入框和消息处理。
// 三个分钟输入框是原生 EDIT 控件，嵌在自绘的圆角框里；其余控件全部自绘。
// 保存后写回 standup.ini 并立即热更新，无需重启。
package main

import (
	"math"
	"strconv"
	"syscall"
	"time"
	"unsafe"
)

const (
	settingsClass = "EliStandUpSettings"

	wsCaption      = 0x00C00000
	wsSysMenu      = 0x00080000
	wsMinimizeBox  = 0x00020000
	wsClipChildren = 0x02000000
	wsChild        = 0x40000000
	wsVisible      = 0x10000000
	wsTabStop      = 0x00010000
	esCenter       = 0x0001
	esAutoHScroll  = 0x0080
	esNumber       = 0x2000
	emSetSel       = 0x00B1
	emLimitText    = 0x00C5
	enSetFocus     = 0x0100
	enKillFocus    = 0x0200
	swShow         = 5
	swRestore      = 9
	idOK           = 1
	idCancel       = 2
	editIDBase     = 101
)

var (
	settingsHwnd uintptr
	settingsSurf *surface
	settingsLay  SettingsLayout
	setEdits     [3]uintptr
	setState     SettingsState
	setTracked   bool
)

var (
	editNames  = [3]string{"提醒间隔", "休息判定", "稍后提醒"}
	editRanges = [3][2]int{{workMin, workMax}, {idleMin, idleMax}, {snoozeMin, snoozeMax}}
)

// showSettings 打开设置窗口；已打开则置前
func showSettings() {
	if settingsHwnd != 0 {
		pShowWindow.Call(settingsHwnd, swRestore)
		pSetForegroundWindow.Call(settingsHwnd)
		return
	}
	s := scale()
	settingsLay = NewSettingsLayout(s)
	setState = SettingsState{Pos: cfg.Position, Sound: cfg.Sound, Auto: cfg.Autostart, Tray: cfg.Tray, Hover: HitNone, Focus: -1}
	setState.Paused = tracker.IsPaused()
	if setState.Paused {
		setState.PauseNote = pauseNote()
	}
	setTracked = false

	style := uintptr(wsCaption | wsSysMenu | wsMinimizeBox | wsClipChildren)
	wr := rect{0, 0, int32(settingsLay.W), int32(settingsLay.H)}
	pAdjustWindowRectEx.Call(uintptr(unsafe.Pointer(&wr)), style, 0, 0)
	ww, wh := wr.Right-wr.Left, wr.Bottom-wr.Top

	var wa rect
	pSystemParametersInfoW.Call(spiWorkAre, 0, uintptr(unsafe.Pointer(&wa)), 0)
	x, y := wa.Left+(wa.Right-wa.Left-ww)/2, wa.Top+(wa.Bottom-wa.Top-wh)/2

	hwnd, _, _ := pCreateWindowExW.Call(0,
		uintptr(unsafe.Pointer(u16(settingsClass))), uintptr(unsafe.Pointer(u16("久坐提醒设置"))),
		style, uintptr(x), uintptr(y), uintptr(ww), uintptr(wh), 0, 0, hInstance, 0)
	if hwnd == 0 {
		msgBox("设置窗口创建失败。", mbOK|mbIconWarn)
		return
	}
	surf := newSurface(int32(settingsLay.W), int32(settingsLay.H))
	if surf == nil {
		pDestroyWindow.Call(hwnd)
		msgBox("设置窗口创建失败（无法创建绘图表面）。", mbOK|mbIconWarn)
		return
	}
	settingsHwnd, settingsSurf = hwnd, surf

	// 输入框：无边框 EDIT 嵌在自绘圆角框内，仅允许数字
	values := [3]time.Duration{cfg.Work, cfg.IdleReset, cfg.Snooze}
	pad, lineH := int32(math.Round(s.F(10))), int32(math.Round(s.F(20)))
	for i, fr := range settingsLay.Edits {
		b := fr.I()
		e, _, _ := pCreateWindowExW.Call(0,
			uintptr(unsafe.Pointer(u16("EDIT"))), uintptr(unsafe.Pointer(u16(strconv.Itoa(int(values[i].Minutes()))))),
			wsChild|wsVisible|wsTabStop|esNumber|esCenter|esAutoHScroll,
			uintptr(int32(b.X)+pad), uintptr(int32(b.Y)+(int32(b.H)-lineH)/2), uintptr(int32(b.W)-2*pad), uintptr(lineH),
			hwnd, uintptr(editIDBase+i), hInstance, 0)
		pSendMessageW.Call(e, wmSetFont, fonts[FontBody], 1)
		pSendMessageW.Call(e, emLimitText, 3, 0)
		setEdits[i] = e
	}

	renderSettings()
	pShowWindow.Call(hwnd, swShow)
	pSetForegroundWindow.Call(hwnd)
	pSetFocus.Call(setEdits[0])
	pSendMessageW.Call(setEdits[0], emSetSel, 0, ^uintptr(0)) // 全选，方便直接改
}

// renderSettings 重绘表面并请求窗口刷新
func renderSettings() {
	if settingsSurf == nil {
		return
	}
	settingsSurf.drawRuns(DrawSettings(settingsSurf.cv, scale(), setState))
	pInvalidateRect.Call(settingsHwnd, 0, 0)
}

func editText(h uintptr) string {
	var buf [8]uint16
	pGetWindowTextW.Call(h, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	return syscall.UTF16ToString(buf[:])
}

// snoozeInput 取输入框中的“稍后提醒”分钟数，非法时回退到当前配置
func snoozeInput() int {
	if n, err := strconv.Atoi(editText(setEdits[2])); err == nil && n >= snoozeMin && n <= snoozeMax {
		return n
	}
	return int(cfg.Snooze.Minutes())
}

// saveSettings 校验 → 写 ini → 热更新 → 同步自启；任一步失败就在窗口内提示，不关窗
func saveSettings() {
	fail := func(msg string) {
		setState.Err = msg
		renderSettings()
	}
	next := cfg
	dst := [3]*time.Duration{&next.Work, &next.IdleReset, &next.Snooze}
	for i, e := range setEdits {
		d, err := ParseMinutes(editNames[i], editText(e), editRanges[i][0], editRanges[i][1])
		if err != nil {
			fail(err.Error())
			pSetFocus.Call(e)
			pSendMessageW.Call(e, emSetSel, 0, ^uintptr(0))
			return
		}
		*dst[i] = d
	}
	next.Position, next.Sound, next.Autostart, next.Tray = setState.Pos, setState.Sound, setState.Auto, setState.Tray

	if err := SaveConfig(iniPath, next); err != nil {
		fail("保存失败：" + err.Error())
		return
	}
	cfg = next
	tracker.UpdateConfig(cfg)
	traySync()
	if _, err := SyncAutostart(cfg.Autostart, exePath); err != nil {
		fail("已保存，但开机自启设置失败：" + err.Error())
		return
	}
	pDestroyWindow.Call(settingsHwnd)
}

func setHover(h int) {
	if h != setState.Hover {
		setState.Hover = h
		renderSettings()
	}
}

func settingsProc(hwnd, msg, wp, lp uintptr) uintptr {
	switch msg {
	case wmEraseBkgnd:
		return 1
	case wmPaint:
		var ps paintStruct
		hdc, _, _ := pBeginPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))
		if settingsSurf != nil {
			pBitBlt.Call(hdc, 0, 0, uintptr(settingsSurf.w), uintptr(settingsSurf.h), settingsSurf.dc, 0, 0, srcCopy)
		}
		pEndPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))
		return 0
	case wmCommand:
		id, code := wp&0xFFFF, (wp>>16)&0xFFFF
		switch {
		case id == idOK: // 回车
			saveSettings()
		case id == idCancel: // Esc
			pDestroyWindow.Call(hwnd)
		case id >= editIDBase && id < editIDBase+3 && (code == enSetFocus || code == enKillFocus):
			setState.Focus = -1
			if code == enSetFocus {
				setState.Focus = int(id - editIDBase)
			}
			renderSettings()
		}
		return 0
	case wmSetCursor:
		if wp == hwnd && lp&0xFFFF == htClient && setState.Hover != HitNone {
			c, _, _ := pLoadCursorW.Call(0, idcHand)
			pSetCursor.Call(c)
			return 1
		}
	case wmMouseMove:
		if !setTracked {
			tme := trackMouseEvent{DwFlags: tmeLeave, HwndTrack: hwnd}
			tme.CbSize = uint32(unsafe.Sizeof(tme))
			pTrackMouseEvent.Call(uintptr(unsafe.Pointer(&tme)))
			setTracked = true
		}
		x, y := lpPoint(lp)
		setHover(settingsLay.HitTest(int(x), int(y)))
		return 0
	case wmMouseLeave:
		setTracked = false
		setHover(HitNone)
		return 0
	case wmLButtonUp:
		x, y := lpPoint(lp)
		hit := settingsLay.HitTest(int(x), int(y))
		switch {
		case hit >= HitPosBase && hit < HitPosBase+len(PositionOrder):
			setState.Pos, setState.Err = PositionOrder[hit-HitPosBase], ""
			renderSettings()
			previewPopup(setState.Pos, snoozeInput(), false) // 选位置即时预览
		case hit == HitSound:
			setState.Sound, setState.Err = !setState.Sound, ""
			renderSettings()
		case hit == HitAuto:
			setState.Auto, setState.Err = !setState.Auto, ""
			renderSettings()
		case hit == HitTray:
			setState.Tray, setState.Err = !setState.Tray, ""
			renderSettings()
		case hit == HitPreview:
			previewPopup(setState.Pos, snoozeInput(), setState.Sound)
		case hit == HitPause:
			pauseButton(hwnd)
		case hit == HitSave:
			saveSettings()
		case hit == HitCancel:
			pDestroyWindow.Call(hwnd)
		case hit == HitQuit:
			pDestroyWindow.Call(hostHwnd) // 宿主销毁 → 退出消息循环 → 进程结束
		}
		return 0
	case wmDestroy:
		settingsSurf.free()
		settingsSurf, settingsHwnd = nil, 0
		if popupHwnd != 0 && popupPreview {
			pDestroyWindow.Call(popupHwnd) // 关闭设置时一并收起预览
		}
		return 0
	}
	r, _, _ := pDefWindowProcW.Call(hwnd, msg, wp, lp)
	return r
}
