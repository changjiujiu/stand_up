//go:build windows

// 提醒弹窗：逐像素 alpha 的分层窗口，自带软阴影与抗锯齿圆角，淡入 + 轻微上滑。
// WS_EX_NOACTIVATE：点击按钮也不会抢走当前窗口的键盘焦点。
package main

import (
	"math"
	"time"
	"unsafe"
)

const (
	wsExLayered    = 0x00080000
	wsExNoActivate = 0x08000000
	ulwAlpha       = 0x2

	timerAnim       = 2
	timerAutoClose  = 3
	animDuration    = 200 * time.Millisecond
	previewDuration = 6000 // 预览弹窗自动关闭（毫秒）
	popupEdge       = 20.0 // 卡片距屏幕边缘（逻辑像素）
	popupSlide      = 12.0 // 入场上滑距离（逻辑像素）
)

type size struct{ CX, CY int32 }

type blendFunction struct{ BlendOp, BlendFlags, SourceConstantAlpha, AlphaFormat byte }

var (
	popupHwnd    uintptr
	popupSurf    *surface
	popupSpec    PopupSpec
	popupLay     PopupLayout
	popupPreview bool
	popupPos     point // 窗口最终位置
	popupAlpha   byte
	popupStart   time.Time
	popupTracked bool
)

func scale() Scale { return Scale{DPI: int(dpi)} }

// showPopup 显示弹窗，返回是否成功。
// 预览弹窗可被新的预览或真实提醒顶替；真实提醒不会被预览顶替。
func showPopup(spec PopupSpec, pos string, beep bool) bool {
	if popupHwnd != 0 {
		if !popupPreview {
			return false
		}
		pDestroyWindow.Call(popupHwnd) // 同步触发 WM_DESTROY，清理状态
	}

	s := scale()
	lay := NewPopupLayout(s, spec.Preview)
	surf := newSurface(int32(lay.W), int32(lay.H))
	if surf == nil {
		return false
	}

	var wa rect
	pSystemParametersInfoW.Call(spiWorkAre, 0, uintptr(unsafe.Pointer(&wa)), 0)
	cx, cy := PopupOrigin(pos, IRect{int(wa.Left), int(wa.Top), int(wa.Right - wa.Left), int(wa.Bottom - wa.Top)},
		lay.Card.W, lay.Card.H, int(math.Round(s.F(popupEdge))))
	x, y := int32(cx-lay.Card.X), int32(cy-lay.Card.Y) // 窗口比卡片大一圈阴影留白

	hwnd, _, _ := pCreateWindowExW.Call(wsExLayered|wsExTopmost|wsExToolWindow|wsExNoActivate,
		uintptr(unsafe.Pointer(u16(popupClass))), uintptr(unsafe.Pointer(u16(appTitle))),
		wsPopup, uintptr(x), uintptr(y), uintptr(lay.W), uintptr(lay.H), 0, 0, hInstance, 0)
	if hwnd == 0 {
		surf.free()
		return false
	}

	popupHwnd, popupSurf, popupLay, popupPreview = hwnd, surf, lay, spec.Preview
	popupSpec = spec
	popupSpec.Hover = PopHitNone
	popupPos, popupAlpha, popupTracked = point{x, y}, 0, false

	renderPopup()
	start := point{x, y + int32(math.Round(s.F(popupSlide)))}
	presentPopup(0, &start) // 先以全透明呈现，再显示窗口，避免闪一下空白
	pShowWindow.Call(hwnd, swShowNoActive)
	popupStart = time.Now()
	pSetTimer.Call(hwnd, timerAnim, 15, 0)
	if spec.Preview {
		pSetTimer.Call(hwnd, timerAutoClose, previewDuration, 0)
	}
	if beep {
		pMessageBeep.Call(mbIconInfo)
	}
	return true
}

// previewPopup 按给定位置显示示例弹窗；不影响计时
func previewPopup(pos string, snoozeMin int, beep bool) {
	showPopup(PopupSpec{SnoozeMin: snoozeMin, Preview: true}, pos, beep)
}

// renderPopup 重绘弹窗内容：形状由 Go 绘制，文字由 GDI 绘制，最后还原 alpha
func renderPopup() {
	cv := popupSurf.cv
	cv.Clear(Color{})
	runs := DrawPopup(cv, scale(), popupSpec)
	popupSurf.alpha = cv.SaveAlpha(popupSurf.alpha)
	popupSurf.drawRuns(runs)
	cv.RestoreAlpha(popupSurf.alpha) // GDI 会破坏 alpha 通道，文字只落在不透明区域，按快照还原
}

// presentPopup 把表面呈现到屏幕；pos 为 nil 时保持窗口当前位置（用户可能已拖动）
func presentPopup(alpha byte, pos *point) {
	sz := size{int32(popupLay.W), int32(popupLay.H)}
	src := point{}
	bf := blendFunction{SourceConstantAlpha: alpha, AlphaFormat: 1 /*AC_SRC_ALPHA*/}
	if pos != nil {
		pUpdateLayeredWindow.Call(popupHwnd, 0, uintptr(unsafe.Pointer(pos)), uintptr(unsafe.Pointer(&sz)),
			popupSurf.dc, uintptr(unsafe.Pointer(&src)), 0, uintptr(unsafe.Pointer(&bf)), ulwAlpha)
		return
	}
	pUpdateLayeredWindow.Call(popupHwnd, 0, 0, uintptr(unsafe.Pointer(&sz)),
		popupSurf.dc, uintptr(unsafe.Pointer(&src)), 0, uintptr(unsafe.Pointer(&bf)), ulwAlpha)
}

// animTick 入场动画：透明度 0→255，同时上滑到位，三次方缓出
func animTick() {
	t := float64(time.Since(popupStart)) / float64(animDuration)
	if t >= 1 {
		t = 1
		pKillTimer.Call(popupHwnd, timerAnim)
	}
	e := 1 - math.Pow(1-t, 3)
	popupAlpha = byte(math.Round(255 * e))
	pos := point{popupPos.X, popupPos.Y + int32(math.Round((1-e)*scale().F(popupSlide)))}
	presentPopup(popupAlpha, &pos)
}

func setPopupHover(h int) {
	if h != popupSpec.Hover {
		popupSpec.Hover = h
		renderPopup()
		presentPopup(popupAlpha, nil)
	}
}

func popupProc(hwnd, msg, wp, lp uintptr) uintptr {
	switch msg {
	case wmNcHitTest:
		x, y := lpPoint(lp)
		pt := point{x, y}
		pScreenToClient.Call(hwnd, uintptr(unsafe.Pointer(&pt)))
		if popupLay.HitTest(int(pt.X), int(pt.Y)) != PopHitNone {
			return htClient
		}
		return htCaption // 按钮以外区域可拖动
	case wmSetCursor:
		if lp&0xFFFF == htClient {
			c, _, _ := pLoadCursorW.Call(0, idcHand)
			pSetCursor.Call(c)
			return 1
		}
	case wmMouseMove:
		if !popupTracked {
			tme := trackMouseEvent{DwFlags: tmeLeave, HwndTrack: hwnd}
			tme.CbSize = uint32(unsafe.Sizeof(tme))
			pTrackMouseEvent.Call(uintptr(unsafe.Pointer(&tme)))
			popupTracked = true
		}
		x, y := lpPoint(lp)
		setPopupHover(popupLay.HitTest(int(x), int(y)))
		return 0
	case wmMouseLeave:
		popupTracked = false
		setPopupHover(PopHitNone)
		return 0
	case wmLButtonUp:
		x, y := lpPoint(lp)
		hit := popupLay.HitTest(int(x), int(y))
		if popupPreview { // 预览：任一按钮都只是关闭
			if hit == PopHitPrimary || hit == PopHitSecondary {
				pDestroyWindow.Call(hwnd)
			}
			return 0
		}
		switch hit {
		case PopHitPrimary:
			tracker.Done(time.Now())
			pDestroyWindow.Call(hwnd)
		case PopHitSecondary:
			tracker.Snooze(time.Now())
			pDestroyWindow.Call(hwnd)
		case PopHitLink:
			showSettings() // 弹窗保留，改完设置再决定
		}
		return 0
	case wmTimer:
		switch wp {
		case timerAnim:
			animTick()
		case timerAutoClose:
			pDestroyWindow.Call(hwnd)
		}
		return 0
	case wmDestroy:
		pKillTimer.Call(hwnd, timerAnim)
		pKillTimer.Call(hwnd, timerAutoClose)
		popupSurf.free()
		popupSurf, popupHwnd = nil, 0
		return 0
	}
	r, _, _ := pDefWindowProcW.Call(hwnd, msg, wp, lp)
	return r
}
