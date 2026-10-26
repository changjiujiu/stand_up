//go:build windows

// 绘制表面：一块 32bpp DIB。Canvas 直接指向 DIB 内存，形状由 Go 绘制，文字由 GDI 绘制。
package main

import "unsafe"

var (
	pCreateDIBSection    = gdi32.NewProc("CreateDIBSection")
	pUpdateLayeredWindow = user32.NewProc("UpdateLayeredWindow")
	pKillTimer           = user32.NewProc("KillTimer")
)

const dtRight = 0x2

// 字体槽位 → HFONT，由 initDPIAndFonts 创建
var fonts [FontCount]uintptr

type bitmapInfoHeader struct {
	Size          uint32
	Width, Height int32
	Planes        uint16
	BitCount      uint16
	Compression   uint32
	SizeImage     uint32
	XPels, YPels  int32
	ClrUsed       uint32
	ClrImportant  uint32
}

type bitmapInfo struct {
	Header bitmapInfoHeader
	Colors [4]byte // 32bpp BI_RGB 不使用调色板，仅为凑齐 BITMAPINFO 的大小
}

type surface struct {
	w, h         int32
	dc, bmp, old uintptr
	cv           *Canvas
	alpha        []byte // 绘制文字前的 alpha 快照
}

// newSurface 创建 w×h 的自上而下 32bpp DIB；失败返回 nil
func newSurface(w, h int32) *surface {
	screen, _, _ := pGetDC.Call(0)
	defer pReleaseDC.Call(0, screen)
	dc, _, _ := pCreateCompatibleDC.Call(screen)
	if dc == 0 {
		return nil
	}
	bi := bitmapInfo{Header: bitmapInfoHeader{Width: w, Height: -h, Planes: 1, BitCount: 32}}
	bi.Header.Size = uint32(unsafe.Sizeof(bi.Header))
	var bits unsafe.Pointer
	bmp, _, _ := pCreateDIBSection.Call(dc, uintptr(unsafe.Pointer(&bi)), 0 /*DIB_RGB_COLORS*/, uintptr(unsafe.Pointer(&bits)), 0, 0)
	if bmp == 0 || bits == nil {
		pDeleteDC.Call(dc)
		return nil
	}
	old, _, _ := pSelectObject.Call(dc, bmp)
	return &surface{
		w: w, h: h, dc: dc, bmp: bmp, old: old,
		cv: &Canvas{W: int(w), H: int(h), Pix: unsafe.Slice((*byte)(bits), int(w)*int(h)*4)},
	}
}

func (s *surface) free() {
	if s == nil {
		return
	}
	pSelectObject.Call(s.dc, s.old)
	pDeleteObject.Call(s.bmp)
	pDeleteDC.Call(s.dc)
}

// drawRuns 用 GDI 把文字叠加到表面上
func (s *surface) drawRuns(runs []TextRun) {
	pSetBkMode.Call(s.dc, 1 /*TRANSPARENT*/)
	for _, t := range runs {
		flags := uintptr(dtNoPrefix)
		if t.Wrap {
			flags |= dtWordBreak // DT_VCENTER 只对单行有效，多行文字顶对齐
		} else {
			flags |= dtSingleLine | dtVCenter
		}
		switch t.Align {
		case AlignCenter:
			flags |= dtCenter
		case AlignRight:
			flags |= dtRight
		}
		drawText(s.dc, fonts[t.Font], t.Color.COLORREF(), t.Text,
			rect{int32(t.R.X), int32(t.R.Y), int32(t.R.X + t.R.W), int32(t.R.Y + t.R.H)}, flags)
	}
}
