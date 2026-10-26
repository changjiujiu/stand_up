//go:build windows

// 暂停提醒：演示、开会前临时关掉提醒，不必退出程序。
// 暂停状态只保存在内存里，不写入配置文件：重启程序即自动恢复，避免忘记取消而一直不提醒。
package main

import (
	"fmt"
	"time"
	"unsafe"
)

const (
	menuResume      = 3
	menuPause30     = 4
	menuPause60     = 5
	menuPause120    = 6
	menuPauseManual = 7
	mfGrayed        = 0x1
)

var pClientToScreen = user32.NewProc("ClientToScreen")

var pauseChoices = [...]struct {
	id    uintptr
	label string
	d     time.Duration // 0 表示直到手动恢复
}{
	{menuPause30, "暂停提醒 30 分钟", 30 * time.Minute},
	{menuPause60, "暂停提醒 1 小时", time.Hour},
	{menuPause120, "暂停提醒 2 小时", 2 * time.Hour},
	{menuPauseManual, "暂停提醒，直到手动恢复", 0},
}

// pauseNote 暂停状态说明，用于托盘提示、菜单和设置窗口页头
func pauseNote() string {
	if until, ok := tracker.PauseUntil(); ok {
		return fmt.Sprintf("已暂停，%s 自动恢复", until.Format("15:04"))
	}
	return "已暂停，需手动恢复"
}

// appendPauseItems 向菜单追加暂停相关项：未暂停时列出时长，已暂停时显示状态与“开启提醒”
func appendPauseItems(menu uintptr) {
	if tracker.IsPaused() {
		pAppendMenuW.Call(menu, mfString|mfGrayed, 0, uintptr(unsafe.Pointer(u16(pauseNote()))))
		pAppendMenuW.Call(menu, mfString, menuResume, uintptr(unsafe.Pointer(u16("开启提醒"))))
		return
	}
	for _, c := range pauseChoices {
		pAppendMenuW.Call(menu, mfString, c.id, uintptr(unsafe.Pointer(u16(c.label))))
	}
}

// handlePauseCmd 处理菜单命令；返回是否属于暂停 / 恢复命令
func handlePauseCmd(cmd uintptr) bool {
	now := time.Now()
	if cmd == menuResume {
		tracker.Resume(now)
		afterPauseChange()
		return true
	}
	for _, c := range pauseChoices {
		if c.id == cmd {
			tracker.Pause(now, c.d)
			afterPauseChange()
			return true
		}
	}
	return false
}

func afterPauseChange() {
	if popupHwnd != 0 && !popupPreview {
		pDestroyWindow.Call(popupHwnd) // 暂停时已弹出的提醒一并收起
	}
	trayUpdateTip()
	syncPauseUI()
}

// syncPauseUI 让设置窗口的暂停状态与状态机保持一致，有变化才重绘
func syncPauseUI() {
	if settingsHwnd == 0 {
		return
	}
	paused, note := tracker.IsPaused(), ""
	if paused {
		note = pauseNote()
	}
	if setState.Paused != paused || setState.PauseNote != note {
		setState.Paused, setState.PauseNote = paused, note
		renderSettings()
	}
}

// pauseButton 设置窗口页头按钮：已暂停则直接恢复，否则在按钮下方弹出时长菜单
func pauseButton(hwnd uintptr) {
	if tracker.IsPaused() {
		handlePauseCmd(menuResume)
		return
	}
	menu, _, _ := pCreatePopupMenu.Call()
	for _, c := range pauseChoices {
		pAppendMenuW.Call(menu, mfString, c.id, uintptr(unsafe.Pointer(u16(c.label))))
	}
	r := settingsLay.Regions[HitPause]
	pt := point{int32(r.X), int32(r.Bottom())}
	pClientToScreen.Call(hwnd, uintptr(unsafe.Pointer(&pt)))
	pSetForegroundWindow.Call(hwnd)
	cmd, _, _ := pTrackPopupMenu.Call(menu, tpmNoNotify|tpmReturnCmd, uintptr(pt.X), uintptr(pt.Y), 0, hwnd, 0)
	pPostMessageW.Call(hwnd, wmNull, 0, 0)
	pDestroyMenu.Call(menu)
	handlePauseCmd(cmd)
}
