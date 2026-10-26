package main

// 与平台无关的软件绘制层：抗锯齿圆角矩形 / 圆 / 线段 / 软阴影。
// 像素格式为 BGRA 预乘，字节序与 Windows 32bpp DIB 一致，
// 因此在 Windows 上可以直接让 Canvas 指向 DIB 内存，无需拷贝。

import (
	"image"
	"math"
)

// Color 非预乘 RGBA 颜色
type Color struct{ R, G, B, A uint8 }

func RGB(r, g, b uint8) Color { return Color{r, g, b, 255} }

func (c Color) WithAlpha(a uint8) Color { c.A = a; return c }

// COLORREF 供 GDI 文本使用（0x00BBGGRR）
func (c Color) COLORREF() uint32 { return uint32(c.R) | uint32(c.G)<<8 | uint32(c.B)<<16 }

// Canvas BGRA 预乘像素缓冲
type Canvas struct {
	W, H int
	Pix  []byte
}

func NewCanvas(w, h int) *Canvas { return &Canvas{W: w, H: h, Pix: make([]byte, w*h*4)} }

func clamp01(v float64) float64 { return math.Max(0, math.Min(1, v)) }

func clamp8(v float64) uint8 {
	v += 0.5
	switch {
	case v <= 0:
		return 0
	case v >= 255:
		return 255
	}
	return uint8(v)
}

// Clear 用 col 填满整张画布（不做混合）
func (c *Canvas) Clear(col Color) {
	a := float64(col.A) / 255
	b, g, r := clamp8(float64(col.B)*a), clamp8(float64(col.G)*a), clamp8(float64(col.R)*a)
	for i := 0; i+3 < len(c.Pix); i += 4 {
		c.Pix[i], c.Pix[i+1], c.Pix[i+2], c.Pix[i+3] = b, g, r, col.A
	}
}

// At 读取像素（预乘 BGRA），供测试使用
func (c *Canvas) At(x, y int) (b, g, r, a uint8) {
	i := (y*c.W + x) * 4
	return c.Pix[i], c.Pix[i+1], c.Pix[i+2], c.Pix[i+3]
}

// blendPx 以覆盖率 cov 把 col 以 src-over 方式叠加到 (x,y)
func (c *Canvas) blendPx(x, y int, col Color, cov float64) {
	a := float64(col.A) / 255 * cov
	if a <= 0 {
		return
	}
	i := (y*c.W + x) * 4
	p := c.Pix[i : i+4 : i+4]
	inv := 1 - a
	p[0] = clamp8(float64(col.B)*a + float64(p[0])*inv)
	p[1] = clamp8(float64(col.G)*a + float64(p[1])*inv)
	p[2] = clamp8(float64(col.R)*a + float64(p[2])*inv)
	p[3] = clamp8(255*a + float64(p[3])*inv)
}

// raster 在包围盒内逐像素求距离场并按 cov 映射成覆盖率后混合
func (c *Canvas) raster(minX, minY, maxX, maxY float64, sdf func(px, py float64) float64, cov func(d float64) float64, col Color) {
	x0, y0 := max(int(math.Floor(minX)), 0), max(int(math.Floor(minY)), 0)
	x1, y1 := min(int(math.Ceil(maxX)), c.W), min(int(math.Ceil(maxY)), c.H)
	for y := y0; y < y1; y++ {
		for x := x0; x < x1; x++ {
			if v := cov(sdf(float64(x)+0.5, float64(y)+0.5)); v > 0 {
				c.blendPx(x, y, col, v)
			}
		}
	}
}

// edgeCov 距离 → 覆盖率：边界处 1px 线性过渡（抗锯齿）
func edgeCov(d float64) float64 { return clamp01(0.5 - d) }

// sdRoundRect 圆角矩形的有符号距离：内部为负，边界为 0，外部为正（单位：像素）
func sdRoundRect(px, py, x, y, w, h, r float64) float64 {
	r = math.Min(r, math.Min(w, h)/2)
	qx := math.Abs(px-(x+w/2)) - (w/2 - r)
	qy := math.Abs(py-(y+h/2)) - (h/2 - r)
	return math.Hypot(math.Max(qx, 0), math.Max(qy, 0)) + math.Min(math.Max(qx, qy), 0) - r
}

// FillRoundRect 填充抗锯齿圆角矩形
func (c *Canvas) FillRoundRect(x, y, w, h, r float64, col Color) {
	c.raster(x-1, y-1, x+w+1, y+h+1, func(px, py float64) float64 { return sdRoundRect(px, py, x, y, w, h, r) }, edgeCov, col)
}

// StrokeRoundRect 描边：线宽 t 画在矩形内侧，因此外轮廓与 FillRoundRect 完全重合
func (c *Canvas) StrokeRoundRect(x, y, w, h, r, t float64, col Color) {
	c.raster(x-1, y-1, x+w+1, y+h+1, func(px, py float64) float64 {
		return math.Abs(sdRoundRect(px, py, x, y, w, h, r)+t/2) - t/2
	}, edgeCov, col)
}

// FillCircle 填充抗锯齿圆
func (c *Canvas) FillCircle(cx, cy, r float64, col Color) {
	c.raster(cx-r-1, cy-r-1, cx+r+1, cy+r+1, func(px, py float64) float64 { return math.Hypot(px-cx, py-cy) - r }, edgeCov, col)
}

// FillCapsule 画两端为圆头的线段，r 为半宽
func (c *Canvas) FillCapsule(ax, ay, bx, by, r float64, col Color) {
	dx, dy := bx-ax, by-ay
	l2 := dx*dx + dy*dy
	c.raster(math.Min(ax, bx)-r-1, math.Min(ay, by)-r-1, math.Max(ax, bx)+r+1, math.Max(ay, by)+r+1,
		func(px, py float64) float64 {
			t := 0.0
			if l2 > 0 {
				t = clamp01(((px-ax)*dx + (py-ay)*dy) / l2)
			}
			return math.Hypot(px-(ax+t*dx), py-(ay+t*dy)) - r
		}, edgeCov, col)
}

// Shadow 圆角矩形的软阴影：offY 为纵向偏移，sigma 为模糊半径（高斯近似）。
// 矩形内部为满强度（通常会被实体覆盖），外部按距离高斯衰减。
func (c *Canvas) Shadow(x, y, w, h, r, offY, sigma float64, col Color) {
	ext := 3 * sigma
	c.raster(x-ext-1, y+offY-ext-1, x+w+ext+1, y+offY+h+ext+1,
		func(px, py float64) float64 { return sdRoundRect(px, py-offY, x, y, w, h, r) },
		func(d float64) float64 {
			if d <= 0 {
				return 1
			}
			return math.Exp(-d * d / (2 * sigma * sigma))
		}, col)
}

// DrawFigure 画“举手伸展的人”，几何与 exe 图标一致；box 为外接正方形
func (c *Canvas) DrawFigure(x, y, size float64, col Color) {
	p := func(u, v float64) (float64, float64) { return x + u*size, y + v*size }
	hx, hy := p(0.5, 0.19)
	c.FillCircle(hx, hy, 0.085*size, col)
	for _, seg := range [][4]float64{
		{0.5, 0.44, 0.5, 0.63},  // 躯干
		{0.5, 0.46, 0.25, 0.24}, // 左臂上举
		{0.5, 0.46, 0.75, 0.24}, // 右臂上举
		{0.5, 0.63, 0.33, 0.85}, // 左腿
		{0.5, 0.63, 0.67, 0.85}, // 右腿
	} {
		ax, ay := p(seg[0], seg[1])
		bx, by := p(seg[2], seg[3])
		c.FillCapsule(ax, ay, bx, by, 0.05*size, col)
	}
}

// SaveAlpha 把 alpha 通道复制到 dst（复用其容量）并返回
func (c *Canvas) SaveAlpha(dst []byte) []byte {
	n := c.W * c.H
	if cap(dst) < n {
		dst = make([]byte, n)
	}
	dst = dst[:n]
	for i := range dst {
		dst[i] = c.Pix[i*4+3]
	}
	return dst
}

// RestoreAlpha 写回 alpha 通道。GDI 绘制文字会破坏 32bpp DIB 的 alpha，
// 文字只落在不透明区域，所以事后按绘制前的快照还原即可
func (c *Canvas) RestoreAlpha(a []byte) {
	for i := range a {
		c.Pix[i*4+3] = a[i]
	}
}

// ToImage 转成标准库图像（预乘 RGBA），用于预览输出和测试
func (c *Canvas) ToImage() *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, c.W, c.H))
	for i := 0; i < c.W*c.H; i++ {
		img.Pix[i*4], img.Pix[i*4+1], img.Pix[i*4+2], img.Pix[i*4+3] = c.Pix[i*4+2], c.Pix[i*4+1], c.Pix[i*4], c.Pix[i*4+3]
	}
	return img
}
