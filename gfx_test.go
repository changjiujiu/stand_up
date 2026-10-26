package main

import "testing"

func alphaAt(c *Canvas, x, y int) uint8 { _, _, _, a := c.At(x, y); return a }

func TestFillRoundRectCoverage(t *testing.T) {
	c := NewCanvas(40, 40)
	c.FillRoundRect(5, 5, 30, 30, 8, RGB(255, 255, 255))
	if a := alphaAt(c, 20, 20); a != 255 {
		t.Fatalf("中心应不透明，实际 alpha=%d", a)
	}
	if a := alphaAt(c, 1, 1); a != 0 {
		t.Fatalf("远处应透明，实际 alpha=%d", a)
	}
	if a := alphaAt(c, 5, 5); a != 0 {
		t.Fatalf("圆角外的角点应透明，实际 alpha=%d", a)
	}
	if a := alphaAt(c, 5, 20); a != 255 {
		t.Fatalf("边缘内侧一像素应不透明，实际 alpha=%d", a)
	}
	if a := alphaAt(c, 4, 20); a != 0 {
		t.Fatalf("边缘外侧一像素应透明，实际 alpha=%d", a)
	}
}

func TestAntiAliasHalfCoverage(t *testing.T) {
	c := NewCanvas(40, 40)
	c.FillRoundRect(5.5, 5.5, 29, 29, 0, RGB(255, 255, 255)) // 边界正好穿过像素中心
	if a := alphaAt(c, 5, 20); a < 125 || a > 130 {
		t.Fatalf("边界像素应约 50%% 覆盖，实际 alpha=%d", a)
	}
}

func TestStrokeStaysInside(t *testing.T) {
	c := NewCanvas(40, 40)
	c.StrokeRoundRect(5, 5, 30, 30, 8, 2, RGB(255, 0, 0))
	if a := alphaAt(c, 5, 20); a != 255 {
		t.Fatalf("描边应在矩形内侧，边缘像素应不透明，实际 alpha=%d", a)
	}
	if a := alphaAt(c, 4, 20); a != 0 {
		t.Fatalf("描边不应超出矩形外轮廓，实际 alpha=%d", a)
	}
	if a := alphaAt(c, 20, 20); a != 0 {
		t.Fatalf("描边内部应为空，实际 alpha=%d", a)
	}
}

func TestBlendOverOpaque(t *testing.T) {
	c := NewCanvas(20, 20)
	c.Clear(RGB(255, 255, 255))
	c.FillRoundRect(2, 2, 16, 16, 0, RGB(255, 0, 0))
	if b, g, r, a := c.At(10, 10); b != 0 || g != 0 || r != 255 || a != 255 {
		t.Fatalf("不透明红应完全覆盖白，实际 BGRA=%d,%d,%d,%d", b, g, r, a)
	}
	c.Clear(RGB(255, 255, 255))
	c.FillRoundRect(2, 2, 16, 16, 0, Color{0, 0, 0, 128})
	if b, g, r, a := c.At(10, 10); a != 255 || b < 125 || b > 130 || g != b || r != b {
		t.Fatalf("50%% 黑叠白应约为 127 灰，实际 BGRA=%d,%d,%d,%d", b, g, r, a)
	}
}

func TestClearIsPremultiplied(t *testing.T) {
	c := NewCanvas(2, 2)
	c.Clear(Color{200, 100, 50, 128})
	b, g, r, a := c.At(0, 0)
	if a != 128 || r != 100 || g != 50 || b != 25 {
		t.Fatalf("预乘结果应为 R=100,G=50,B=25,A=128，实际 R=%d,G=%d,B=%d,A=%d", r, g, b, a)
	}
}

func TestShadowFalloff(t *testing.T) {
	c := NewCanvas(80, 120)
	c.Shadow(20, 20, 40, 40, 8, 0, 8, Color{0, 0, 0, 255})
	a1, a2, a3 := alphaAt(c, 40, 61), alphaAt(c, 40, 66), alphaAt(c, 40, 76)
	if !(a1 > a2 && a2 > a3 && a3 > 0) {
		t.Fatalf("阴影应随距离单调衰减，实际 %d > %d > %d", a1, a2, a3)
	}
	if a := alphaAt(c, 40, 110); a > 2 {
		t.Fatalf("三倍 sigma 之外应几乎透明，实际 alpha=%d", a)
	}
}

func TestShadowOffsetMovesDown(t *testing.T) {
	c := NewCanvas(80, 120)
	c.Shadow(20, 40, 40, 20, 4, 8, 4, Color{0, 0, 0, 255})
	if up, down := alphaAt(c, 40, 34), alphaAt(c, 40, 74); down <= up {
		t.Fatalf("向下偏移后，下方阴影应更浓：上=%d 下=%d", up, down)
	}
}

func TestCircleAndCapsule(t *testing.T) {
	c := NewCanvas(40, 40)
	c.FillCircle(20, 20, 8, RGB(0, 0, 0))
	if alphaAt(c, 20, 20) != 255 || alphaAt(c, 20, 5) != 0 {
		t.Fatal("圆内应不透明、圆外应透明")
	}
	c2 := NewCanvas(40, 40)
	c2.FillCapsule(10, 20, 30, 20, 3, RGB(0, 0, 0))
	if alphaAt(c2, 20, 20) != 255 || alphaAt(c2, 20, 26) != 0 || alphaAt(c2, 8, 20) == 0 {
		t.Fatal("线段中部应不透明、侧面外应透明、端点应有圆头")
	}
}

func TestSaveRestoreAlpha(t *testing.T) {
	c := NewCanvas(4, 4)
	c.Clear(RGB(10, 20, 30))
	snap := c.SaveAlpha(nil)
	for i := 0; i < len(c.Pix); i += 4 {
		c.Pix[i+3] = 0 // 模拟 GDI 把 alpha 写坏
	}
	c.RestoreAlpha(snap)
	if alphaAt(c, 2, 2) != 255 {
		t.Fatal("alpha 应被还原")
	}
}
