//go:build windows

// 久坐提醒：按键鼠活动计时，连续使用满 N 分钟弹出提醒。
// 仅依赖 Go 标准库 + Windows 自带 DLL（user32 / gdi32 / kernel32 / dwmapi）。
package main

import (
	"math"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

// ---------------- Win32 绑定 ----------------

var (
	user32   = syscall.NewLazyDLL("user32.dll")
	gdi32    = syscall.NewLazyDLL("gdi32.dll")
	kernel32 = syscall.NewLazyDLL("kernel32.dll")
	dwmapi   = syscall.NewLazyDLL("dwmapi.dll")

	pRegisterClassExW      = user32.NewProc("RegisterClassExW")
	pCreateWindowExW       = user32.NewProc("CreateWindowExW")
	pDefWindowProcW        = user32.NewProc("DefWindowProcW")
	pDestroyWindow         = user32.NewProc("DestroyWindow")
	pShowWindow            = user32.NewProc("ShowWindow")
	pGetMessageW           = user32.NewProc("GetMessageW")
	pTranslateMessage      = user32.NewProc("TranslateMessage")
	pDispatchMessageW      = user32.NewProc("DispatchMessageW")
	pPostQuitMessage       = user32.NewProc("PostQuitMessage")
	pPostMessageW          = user32.NewProc("PostMessageW")
	pSetTimer              = user32.NewProc("SetTimer")
	pBeginPaint            = user32.NewProc("BeginPaint")
	pEndPaint              = user32.NewProc("EndPaint")
	pInvalidateRect        = user32.NewProc("InvalidateRect")
	pGetClientRect         = user32.NewProc("GetClientRect")
	pGetLastInputInfo      = user32.NewProc("GetLastInputInfo")
	pLoadCursorW           = user32.NewProc("LoadCursorW")
	pSetCursor             = user32.NewProc("SetCursor")
	pFillRect              = user32.NewProc("FillRect")
	pFrameRect             = user32.NewProc("FrameRect")
	pDrawTextW             = user32.NewProc("DrawTextW")
	pFindWindowW           = user32.NewProc("FindWindowW")
	pMessageBoxW           = user32.NewProc("MessageBoxW")
	pSystemParametersInfoW = user32.NewProc("SystemParametersInfoW")
	pSetProcessDPIAware    = user32.NewProc("SetProcessDPIAware")
	pMessageBeep           = user32.NewProc("MessageBeep")
	pGetDC                 = user32.NewProc("GetDC")
	pReleaseDC             = user32.NewProc("ReleaseDC")
	pScreenToClient        = user32.NewProc("ScreenToClient")
	pTrackMouseEvent       = user32.NewProc("TrackMouseEvent")
	pAllowSetForeground    = user32.NewProc("AllowSetForegroundWindow")
	pSetForegroundWindow   = user32.NewProc("SetForegroundWindow")
	pIsDialogMessageW      = user32.NewProc("IsDialogMessageW")
	pAdjustWindowRectEx    = user32.NewProc("AdjustWindowRectEx")
	pGetWindowTextW        = user32.NewProc("GetWindowTextW")
	pSetWindowTextW        = user32.NewProc("SetWindowTextW")
	pSendMessageW          = user32.NewProc("SendMessageW")
	pSetFocus              = user32.NewProc("SetFocus")

	pCreateFontW            = gdi32.NewProc("CreateFontW")
	pSelectObject           = gdi32.NewProc("SelectObject")
	pDeleteObject           = gdi32.NewProc("DeleteObject")
	pCreateSolidBrush       = gdi32.NewProc("CreateSolidBrush")
	pRoundRect              = gdi32.NewProc("RoundRect")
	pSetBkMode              = gdi32.NewProc("SetBkMode")
	pSetTextColor           = gdi32.NewProc("SetTextColor")
	pGetDeviceCaps          = gdi32.NewProc("GetDeviceCaps")
	pCreateCompatibleDC     = gdi32.NewProc("CreateCompatibleDC")
	pCreateCompatibleBitmap = gdi32.NewProc("CreateCompatibleBitmap")
	pBitBlt                 = gdi32.NewProc("BitBlt")
	pDeleteDC               = gdi32.NewProc("DeleteDC")
	pGetStockObject         = gdi32.NewProc("GetStockObject")

	pGetTickCount     = kernel32.NewProc("GetTickCount")
	pCreateMutexW     = kernel32.NewProc("CreateMutexW")
	pGetModuleHandleW = kernel32.NewProc("GetModuleHandleW")

	pDwmSetWindowAttribute = dwmapi.NewProc("DwmSetWindowAttribute")
)

const (
	wsPopup        = 0x80000000
	wsExTopmost    = 0x00000008
	wsExToolWindow = 0x00000080
	csDropShadow   = 0x00020000
	swShowNoActive = 4

	wmDestroy    = 0x0002
	wmPaint      = 0x000F
	wmClose      = 0x0010
	wmEraseBkgnd = 0x0014
	wmSetCursor  = 0x0020
	wmNcHitTest  = 0x0084
	wmTimer      = 0x0113
	wmMouseMove  = 0x0200
	wmLButtonUp  = 0x0202
	wmMouseLeave = 0x02A3
	wmCommand    = 0x0111
	wmSetFont    = 0x0030
	wmAppSetting = 0x8001 // WM_APP+1：请求打开设置窗口

	htClient   = 1
	htCaption  = 2
	idcArrow   = 32512
	idcHand    = 32649
	tmeLeave   = 0x2
	spiWorkAre = 0x0030

	dtLeft       = 0x000
	dtCenter     = 0x001
	dtVCenter    = 0x004
	dtWordBreak  = 0x010
	dtSingleLine = 0x020
	dtNoPrefix   = 0x800

	mbOK          = 0x00000000
	mbYesNo       = 0x00000004
	mbIconQuest   = 0x00000020
	mbIconWarn    = 0x00000030
	mbIconInfo    = 0x00000040
	mbTopmost     = 0x00040000
	idYes         = 6
	errAlreadyExi = 183

	nullPen      = 8
	srcCopy      = 0x00CC0020
	logPixelsX   = 88
	timerID      = 1
	tickInterval = 15 * time.Second
)

type point struct{ X, Y int32 }
type rect struct{ Left, Top, Right, Bottom int32 }

type msgT struct {
	Hwnd    uintptr
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pt      point
	Private uint32
}

type wndClassEx struct {
	CbSize        uint32
	Style         uint32
	LpfnWndProc   uintptr
	CbClsExtra    int32
	CbWndExtra    int32
	HInstance     uintptr
	HIcon         uintptr
	HCursor       uintptr
	HbrBackground uintptr
	LpszMenuName  *uint16
	LpszClassName *uint16
	HIconSm       uintptr
}

type paintStruct struct {
	Hdc         uintptr
	FErase      int32
	RcPaint     rect
	FRestore    int32
	FIncUpdate  int32
	RgbReserved [32]byte
}

type lastInputInfo struct {
	CbSize uint32
	DwTime uint32
}

type trackMouseEvent struct {
	CbSize      uint32
	DwFlags     uint32
	HwndTrack   uintptr
	DwHoverTime uint32
}

// ---------------- 全局状态（全部在主 UI 线程访问） ----------------

const (
	appTitle   = "久坐提醒"
	hostClass  = "EliStandUpHost"
	popupClass = "EliStandUpPopup"
)

var (
	hInstance uintptr
	hostHwnd  uintptr
	dpi       int32 = 96

	cfg     Config
	tracker *Tracker
	exePath string
	iniPath string
)

func u16(s string) *uint16 {
	p, err := syscall.UTF16PtrFromString(s)
	if err != nil {
		p, _ = syscall.UTF16PtrFromString("?")
	}
	return p
}

func msgBox(text string, flags uintptr) uintptr {
	r, _, _ := pMessageBoxW.Call(0, uintptr(unsafe.Pointer(u16(text))), uintptr(unsafe.Pointer(u16(appTitle))), flags|mbTopmost)
	return r
}

// ---------------- 入口 ----------------

func main() {
	runtime.LockOSThread() // Win32 窗口与消息循环必须在同一线程

	var err error
	exePath, err = os.Executable()
	if err != nil {
		msgBox("无法定位程序路径："+err.Error(), mbOK|mbIconWarn)
		return
	}

	// 单实例：已在运行时，让旧实例打开设置窗口（没有托盘，这就是设置与退出的入口）
	h, _, e := pCreateMutexW.Call(0, 0, uintptr(unsafe.Pointer(u16(`Local\EliStandUpReminder`))))
	if h != 0 && e == syscall.Errno(errAlreadyExi) {
		if hw, _, _ := pFindWindowW.Call(uintptr(unsafe.Pointer(u16(hostClass))), 0); hw != 0 {
			pAllowSetForeground.Call(0xFFFFFFFF) // ASFW_ANY：允许旧实例把设置窗口置前
			pPostMessageW.Call(hw, wmAppSetting, 0, 0)
		}
		return
	}

	iniPath = filepath.Join(filepath.Dir(exePath), "standup.ini")
	var warns []string
	var created bool
	cfg, warns, created, err = LoadConfig(iniPath)
	if err != nil {
		warns = append(warns, "配置文件读写失败，使用默认值："+err.Error())
	}
	if note, err := SyncAutostart(cfg.Autostart, exePath); err != nil {
		warns = append(warns, "开机自启设置失败："+err.Error())
	} else if note != "" {
		msgBox(note, mbOK|mbIconInfo)
	}

	pSetProcessDPIAware.Call() // 避免高分屏下被系统位图拉伸发虚
	initDPIAndFonts()

	hInstance, _, _ = pGetModuleHandleW.Call(0)
	loadIcons()
	registerClass(hostClass, hostProc, 0)
	registerClass(popupClass, popupProc, 0)
	registerClass(settingsClass, settingsProc, 0)
	hostHwnd, _, _ = pCreateWindowExW.Call(0, uintptr(unsafe.Pointer(u16(hostClass))), uintptr(unsafe.Pointer(u16(appTitle))),
		0, 0, 0, 0, 0, 0, 0, hInstance, 0) // 隐藏宿主窗口：承载定时器，并供“再次运行退出”查找
	if hostHwnd == 0 {
		msgBox("窗口创建失败，程序退出。", mbOK|mbIconWarn)
		return
	}

	tracker = NewTracker(cfg, time.Now())
	traySync()
	pSetTimer.Call(hostHwnd, timerID, uintptr(tickInterval/time.Millisecond), 0)

	if len(warns) > 0 {
		msgBox("配置提示：\n\n"+strings.Join(warns, "\n"), mbOK|mbIconWarn)
	}
	if created {
		showSettings() // 首次运行直接打开设置
	}

	var m msgT
	for {
		r, _, _ := pGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(r) <= 0 {
			break
		}
		if settingsHwnd != 0 {
			if r, _, _ := pIsDialogMessageW.Call(settingsHwnd, uintptr(unsafe.Pointer(&m))); r != 0 {
				continue
			}
		}
		pTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		pDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
	}
}

func registerClass(name string, proc func(hwnd, msg, wp, lp uintptr) uintptr, style uint32) {
	cursor, _, _ := pLoadCursorW.Call(0, idcArrow)
	wc := wndClassEx{
		Style:         style,
		LpfnWndProc:   syscall.NewCallback(proc),
		HInstance:     hInstance,
		HCursor:       cursor,
		HIcon:         hIconLarge,
		HIconSm:       hIconSmall,
		LpszClassName: u16(name),
	}
	wc.CbSize = uint32(unsafe.Sizeof(wc))
	pRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))
}

func initDPIAndFonts() {
	if hdc, _, _ := pGetDC.Call(0); hdc != 0 {
		if v, _, _ := pGetDeviceCaps.Call(hdc, logPixelsX); v != 0 {
			dpi = int32(v)
		}
		pReleaseDC.Call(0, hdc)
	}
	fonts[FontTitle] = makeFont(20, 700)
	fonts[FontBody] = makeFont(14, 400)
	fonts[FontButton] = makeFont(14, 600)
	fonts[FontSmall] = makeFont(13, 400)
	fonts[FontSection] = makeFont(15, 700)
	fonts[FontCaption] = makeFont(12, 600)
}

func makeFont(px, weight int32) uintptr {
	h, _, _ := pCreateFontW.Call(uintptr(-int32(math.Round(scale().F(float64(px))))), 0, 0, 0, uintptr(weight), 0, 0, 0,
		1 /*DEFAULT_CHARSET*/, 0, 0, 5 /*CLEARTYPE_QUALITY*/, 0, uintptr(unsafe.Pointer(u16("Microsoft YaHei UI"))))
	return h
}

// ---------------- 定时轮询 ----------------

func idleDuration() time.Duration {
	lii := lastInputInfo{CbSize: uint32(unsafe.Sizeof(lastInputInfo{}))}
	if r, _, _ := pGetLastInputInfo.Call(uintptr(unsafe.Pointer(&lii))); r == 0 {
		return 0 // 取不到时按“活跃”处理，宁可多提醒不漏提醒
	}
	tick, _, _ := pGetTickCount.Call()
	return time.Duration(uint32(tick)-lii.DwTime) * time.Millisecond // uint32 回绕减法，49.7 天溢出也正确
}

func onTick() {
	now := time.Now()
	defer func() {
		traySync() // 开机时 Explorer 未就绪导致添加失败的，在这里重试
		trayUpdateTip()
	}()
	switch tracker.Tick(now, idleDuration()) {
	case ActShow:
		ok := showPopup(PopupSpec{Minutes: int(tracker.ActiveFor(now).Minutes()), SnoozeMin: int(cfg.Snooze.Minutes())}, cfg.Position, cfg.Sound)
		if !ok {
			tracker.Snooze(now) // 窗口创建失败：别卡在“已弹出”状态，稍后重试
		}
	case ActAutoDismiss:
		if popupHwnd != 0 && !popupPreview { // 预览弹窗不受“人已离开”影响
			pDestroyWindow.Call(popupHwnd)
		}
	}
}

func hostProc(hwnd, msg, wp, lp uintptr) uintptr {
	if msg == taskbarCreatedMsg && msg != 0 { // Explorer 重启
		trayAdded = false
		traySync()
		return 0
	}
	switch msg {
	case wmTimer:
		onTick()
		return 0
	case wmAppSetting:
		showSettings()
		return 0
	case wmAppTray:
		onTrayEvent(lp)
		return 0
	case wmClose:
		pDestroyWindow.Call(hwnd)
		return 0
	case wmDestroy:
		trayRemove() // 不删除会在任务栏残留“幽灵图标”
		pPostQuitMessage.Call(0)
		return 0
	}
	r, _, _ := pDefWindowProcW.Call(hwnd, msg, wp, lp)
	return r
}

// ---------------- 工具 ----------------

func lpPoint(lp uintptr) (int32, int32) {
	return int32(int16(lp & 0xFFFF)), int32(int16((lp >> 16) & 0xFFFF))
}

func drawText(dc, font uintptr, color uint32, text string, r rect, flags uintptr) {
	old, _, _ := pSelectObject.Call(dc, font)
	pSetTextColor.Call(dc, uintptr(color))
	pDrawTextW.Call(dc, uintptr(unsafe.Pointer(u16(text))), ^uintptr(0) /* -1：以 \0 结尾 */, uintptr(unsafe.Pointer(&r)), flags)
	pSelectObject.Call(dc, old)
}
