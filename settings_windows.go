//go:build windows

// 设置窗口：三个分钟输入框 + 弹窗位置 + 提示音 / 开机自启开关。
// 保存后写回 standup.ini 并立即热更新，无需重启。
package main

import (
	"strconv"
	"syscall"
	"time"
	"unsafe"
)

const (
	settingsClass = "EliStandUpSettings"

	wsOverlappedCap = 0x00C00000 // WS_CAPTION
	wsSysMenu       = 0x00080000
	wsMinimizeBox   = 0x00020000
	wsClipChildren  = 0x02000000
	wsChild         = 0x40000000
	wsVisible       = 0x10000000
	wsTabStop       = 0x00010000
	esCenter        = 0x0001
	esAutoHScroll   = 0x0080
	esNumber        = 0x2000
	emSetSel        = 0x00B1
	emLimitText     = 0x00C5
	enSetFocus      = 0x0100
	enKillFocus     = 0x0200
	swShow          = 5
	swRestore       = 9
	idOK            = 1
	idCancel        = 2
	editIDBase      = 101
)

// 可点击区域
const (
	hitCorner = iota
	hitCenter
	hitSound
	hitAuto
	hitTray
	hitSave
	hitCancel
	hitQuit
	hitCount
)

var (
	colInputBorder = rgb(0xCB, 0xD2, 0xD9)
	colToggleOff   = rgb(0xC5, 0xCB, 0xD1)
	colDanger      = rgb(0xC6, 0x28, 0x28)
)

var (
	settingsHwnd uintptr
	setEdits     [3]uintptr
	setBoxes     [3]rect
	setRegions   [hitCount]rect
	setTracks    [3]rect // 两个开关的绘制位置（点击区域是整行）

	setPos                     string
	setSound, setAuto, setTray bool
	setHover                   = -1
	setFocus                   = -1
	setTracked                 bool
	setErr                     string
)

var (
	rowLabels   = [3]string{"连续使用多久后提醒", "离开多久算已休息", "稍后提醒的间隔"}
	rowNames    = [3]string{"提醒间隔", "休息判定", "稍后提醒"}
	rowRanges   = [3][2]int{{workMin, workMax}, {idleMin, idleMax}, {snoozeMin, snoozeMax}}
	rowCenters  = [3]int32{112, 156, 200} // 96 DPI 下的行中心 y
	settingsW   = int32(420)
	settingsH   = int32(520)
	otherCenter = [4]int32{290, 334, 374, 414} // 弹窗位置 / 提示音 / 开机自启 / 任务栏图标
)

func layoutSettings() {
	right := sc(settingsW - 28)
	for i, c := range rowCenters {
		setBoxes[i] = rect{right - sc(44) - sc(72), sc(c - 16), right - sc(44), sc(c + 16)}
	}
	seg := rect{right - sc(176), sc(otherCenter[0] - 16), right, sc(otherCenter[0] + 16)}
	mid := (seg.Left + seg.Right) / 2
	setRegions[hitCorner] = rect{seg.Left, seg.Top, mid, seg.Bottom}
	setRegions[hitCenter] = rect{mid, seg.Top, seg.Right, seg.Bottom}
	for i, c := range otherCenter[1:] {
		setTracks[i] = rect{right - sc(44), sc(c - 12), right, sc(c + 12)}
		setRegions[hitSound+i] = rect{sc(28), sc(c - 18), right, sc(c + 18)} // 点整行即可切换
	}
	setRegions[hitSave] = rect{right - sc(96), sc(464), right, sc(500)}
	setRegions[hitCancel] = rect{right - sc(202), sc(464), right - sc(106), sc(500)}
	setRegions[hitQuit] = rect{sc(28), sc(464), sc(120), sc(500)}
}

// showSettings 打开设置窗口；已打开则置前
func showSettings() {
	if settingsHwnd != 0 {
		pShowWindow.Call(settingsHwnd, swRestore)
		pSetForegroundWindow.Call(settingsHwnd)
		return
	}
	setPos, setSound, setAuto, setTray = cfg.Position, cfg.Sound, cfg.Autostart, cfg.Tray
	setHover, setFocus, setTracked, setErr = -1, -1, false, ""

	style := uintptr(wsOverlappedCap | wsSysMenu | wsMinimizeBox | wsClipChildren)
	wr := rect{0, 0, sc(settingsW), sc(settingsH)}
	pAdjustWindowRectEx.Call(uintptr(unsafe.Pointer(&wr)), style, 0, 0)
	ww, wh := wr.Right-wr.Left, wr.Bottom-wr.Top

	var wa rect
	pSystemParametersInfoW.Call(spiWorkAre, 0, uintptr(unsafe.Pointer(&wa)), 0)
	x, y := wa.Left+(wa.Right-wa.Left-ww)/2, wa.Top+(wa.Bottom-wa.Top-wh)/2

	settingsHwnd, _, _ = pCreateWindowExW.Call(0,
		uintptr(unsafe.Pointer(u16(settingsClass))), uintptr(unsafe.Pointer(u16("久坐提醒设置"))),
		style, uintptr(x), uintptr(y), uintptr(ww), uintptr(wh), 0, 0, hInstance, 0)
	if settingsHwnd == 0 {
		msgBox("设置窗口创建失败。", mbOK|mbIconWarn)
		return
	}
	layoutSettings()

	// 输入框：无边框 EDIT 嵌在自绘圆角框内，仅允许数字
	values := [3]time.Duration{cfg.Work, cfg.IdleReset, cfg.Snooze}
	lineH := sc(20)
	for i, b := range setBoxes {
		e, _, _ := pCreateWindowExW.Call(0,
			uintptr(unsafe.Pointer(u16("EDIT"))), uintptr(unsafe.Pointer(u16(strconv.Itoa(int(values[i].Minutes()))))),
			wsChild|wsVisible|wsTabStop|esNumber|esCenter|esAutoHScroll,
			uintptr(b.Left+sc(6)), uintptr(b.Top+(b.Bottom-b.Top-lineH)/2), uintptr(b.Right-b.Left-sc(12)), uintptr(lineH),
			settingsHwnd, uintptr(editIDBase+i), hInstance, 0)
		pSendMessageW.Call(e, wmSetFont, fontBody, 1)
		pSendMessageW.Call(e, emLimitText, 3, 0)
		setEdits[i] = e
	}

	pShowWindow.Call(settingsHwnd, swShow)
	pSetForegroundWindow.Call(settingsHwnd)
	pSetFocus.Call(setEdits[0])
	pSendMessageW.Call(setEdits[0], emSetSel, 0, ^uintptr(0)) // 全选，方便直接改
}

func editText(h uintptr) string {
	var buf [8]uint16
	pGetWindowTextW.Call(h, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	return syscall.UTF16ToString(buf[:])
}

// saveSettings 校验 → 写 ini → 热更新 → 同步自启；任一步失败就在窗口内提示，不关窗
func saveSettings() {
	next := cfg
	dst := [3]*time.Duration{&next.Work, &next.IdleReset, &next.Snooze}
	for i, e := range setEdits {
		d, err := ParseMinutes(rowNames[i], editText(e), rowRanges[i][0], rowRanges[i][1])
		if err != nil {
			setErr = err.Error()
			pInvalidateRect.Call(settingsHwnd, 0, 0)
			pSetFocus.Call(e)
			pSendMessageW.Call(e, emSetSel, 0, ^uintptr(0))
			return
		}
		*dst[i] = d
	}
	next.Position, next.Sound, next.Autostart, next.Tray = setPos, setSound, setAuto, setTray

	if err := SaveConfig(iniPath, next); err != nil {
		setErr = "保存失败：" + err.Error()
		pInvalidateRect.Call(settingsHwnd, 0, 0)
		return
	}
	cfg = next
	tracker.UpdateConfig(cfg)
	traySync()
	if popupHwnd != 0 {
		pInvalidateRect.Call(popupHwnd, 0, 0) // 弹窗上“X 分钟后提醒”的文案可能变了
	}
	if _, err := SyncAutostart(cfg.Autostart, exePath); err != nil {
		setErr = "已保存，但开机自启设置失败：" + err.Error()
		pInvalidateRect.Call(settingsHwnd, 0, 0)
		return
	}
	pDestroyWindow.Call(settingsHwnd)
}

func hitSettings(x, y int32) int {
	for i, r := range setRegions {
		if x >= r.Left && x < r.Right && y >= r.Top && y < r.Bottom {
			return i
		}
	}
	return -1
}

func settingsProc(hwnd, msg, wp, lp uintptr) uintptr {
	switch msg {
	case wmEraseBkgnd:
		return 1
	case wmPaint:
		paintSettings(hwnd)
		return 0
	case wmCommand:
		id, code := wp&0xFFFF, (wp>>16)&0xFFFF
		switch {
		case id == idOK: // 回车
			saveSettings()
		case id == idCancel: // Esc
			pDestroyWindow.Call(hwnd)
		case id >= editIDBase && id < editIDBase+3 && (code == enSetFocus || code == enKillFocus):
			setFocus = -1
			if code == enSetFocus {
				setFocus = int(id - editIDBase)
			}
			pInvalidateRect.Call(hwnd, 0, 0)
		}
		return 0
	case wmSetCursor:
		if wp == hwnd && lp&0xFFFF == htClient && setHover >= 0 {
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
		if i := hitSettings(lpPoint(lp)); i != setHover {
			setHover = i
			pInvalidateRect.Call(hwnd, 0, 0)
		}
		return 0
	case wmMouseLeave:
		setTracked, setHover = false, -1
		pInvalidateRect.Call(hwnd, 0, 0)
		return 0
	case wmLButtonUp:
		switch hitSettings(lpPoint(lp)) {
		case hitCorner:
			setPos = "corner"
		case hitCenter:
			setPos = "center"
		case hitSound:
			setSound = !setSound
		case hitAuto:
			setAuto = !setAuto
		case hitTray:
			setTray = !setTray
		case hitSave:
			saveSettings()
			return 0
		case hitCancel:
			pDestroyWindow.Call(hwnd)
			return 0
		case hitQuit:
			pDestroyWindow.Call(hostHwnd) // 宿主销毁 → 退出消息循环 → 进程结束
			return 0
		}
		pInvalidateRect.Call(hwnd, 0, 0)
		return 0
	case wmDestroy:
		settingsHwnd = 0
		return 0
	}
	r, _, _ := pDefWindowProcW.Call(hwnd, msg, wp, lp)
	return r
}

func paintSettings(hwnd uintptr) {
	var ps paintStruct
	hdc, _, _ := pBeginPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))
	defer pEndPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))

	var rc rect
	pGetClientRect.Call(hwnd, uintptr(unsafe.Pointer(&rc)))
	w, h := rc.Right, rc.Bottom
	mem, _, _ := pCreateCompatibleDC.Call(hdc)
	bmp, _, _ := pCreateCompatibleBitmap.Call(hdc, uintptr(w), uintptr(h))
	oldBmp, _, _ := pSelectObject.Call(mem, bmp)
	defer func() {
		pSelectObject.Call(mem, oldBmp)
		pDeleteObject.Call(bmp)
		pDeleteDC.Call(mem)
	}()

	fill(mem, rc, colBg, false)
	pSetBkMode.Call(mem, 1)
	left, right := sc(28), w-sc(28)
	single := uintptr(dtSingleLine | dtVCenter | dtNoPrefix)

	drawText(mem, fontSmall, colBody, "保存后立即生效。单击任务栏托盘图标，或再次打开 StandUp.exe，即可回到这里。",
		rect{left, sc(16), right, sc(56)}, dtLeft|dtWordBreak|dtNoPrefix)

	// —— 提醒规则 ——
	drawText(mem, fontSection, colTitle, "提醒规则", rect{left, sc(66), right, sc(90)}, dtLeft|single)
	for i, b := range setBoxes {
		drawText(mem, fontBody, colTitle, rowLabels[i], rect{left, b.Top, b.Left - sc(8), b.Bottom}, dtLeft|single)
		border := colInputBorder
		if i == setFocus {
			border = colAccent
		}
		roundRect(mem, b, border, sc(6))
		roundRect(mem, rect{b.Left + 1, b.Top + 1, b.Right - 1, b.Bottom - 1}, colBg, sc(6))
		drawText(mem, fontBody, colBody, "分钟", rect{b.Right + sc(8), b.Top, right, b.Bottom}, dtLeft|single)
	}

	fill(mem, rect{left, sc(232), right, sc(233)}, colBorder, false)

	// —— 其他 ——
	drawText(mem, fontSection, colTitle, "其他", rect{left, sc(244), right, sc(268)}, dtLeft|single)
	others := [4]string{"弹窗位置", "提示音", "开机自启", "任务栏图标"}
	for i, c := range otherCenter {
		drawText(mem, fontBody, colTitle, others[i], rect{left, sc(c - 16), right, sc(c + 16)}, dtLeft|single)
	}

	// 分段选择：右下角 / 屏幕中央
	seg := rect{setRegions[hitCorner].Left, setRegions[hitCorner].Top, setRegions[hitCenter].Right, setRegions[hitCenter].Bottom}
	roundRect(mem, seg, colSecondary, sc(8))
	for i, label := range [2]string{"右下角", "屏幕中央"} {
		r := setRegions[hitCorner+i]
		inner := rect{r.Left + sc(3), r.Top + sc(3), r.Right - sc(3), r.Bottom - sc(3)}
		selected := (i == 0) == (setPos == "corner")
		fg := colOnSecondary
		switch {
		case selected:
			roundRect(mem, inner, colAccent, sc(6))
			fg = colOnAccent
		case setHover == hitCorner+i:
			roundRect(mem, inner, colSecondaryHv, sc(6))
		}
		drawText(mem, fontBody, fg, label, r, dtCenter|single)
	}

	// 开关：提示音 / 开机自启
	for i, on := range [3]bool{setSound, setAuto, setTray} {
		t := setTracks[i]
		track := colToggleOff
		if on {
			track = colAccent
		}
		roundRect(mem, t, track, t.Bottom-t.Top)
		d := (t.Bottom - t.Top) - sc(6)
		kx := t.Left + sc(3)
		if on {
			kx = t.Right - sc(3) - d
		}
		roundRect(mem, rect{kx, t.Top + sc(3), kx + d, t.Top + sc(3) + d}, colBg, d)
	}

	if setErr != "" {
		drawText(mem, fontSmall, colDanger, setErr, rect{left, sc(436), right, sc(458)}, dtLeft|single)
	}

	// 底部按钮
	saveBg, cancelBg := colAccent, colSecondary
	if setHover == hitSave {
		saveBg = colAccentHover
	}
	if setHover == hitCancel {
		cancelBg = colSecondaryHv
	}
	roundRect(mem, setRegions[hitSave], saveBg, sc(8))
	drawText(mem, fontButton, colOnAccent, "保存", setRegions[hitSave], dtCenter|single)
	roundRect(mem, setRegions[hitCancel], cancelBg, sc(8))
	drawText(mem, fontButton, colOnSecondary, "取消", setRegions[hitCancel], dtCenter|single)
	quitColor := colBody
	if setHover == hitQuit {
		quitColor = colDanger
	}
	drawText(mem, fontBody, quitColor, "退出程序", setRegions[hitQuit], dtLeft|single)

	pBitBlt.Call(hdc, 0, 0, uintptr(w), uintptr(h), mem, 0, 0, srcCopy)
}
