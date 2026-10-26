package main

import (
	"encoding/json"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPopupOrigin(t *testing.T) {
	wa := IRect{0, 0, 1920, 1080}
	cases := map[string][2]int{
		"top_left":     {20, 20},
		"top_right":    {1500, 20},
		"bottom_left":  {20, 876},
		"bottom_right": {1500, 876},
		"center":       {760, 448},
		"bogus":        {1500, 876}, // 未知值回退到右下角
	}
	for pos, want := range cases {
		if x, y := PopupOrigin(pos, wa, 400, 184, 20); x != want[0] || y != want[1] {
			t.Errorf("%s：期望 %v，实际 (%d,%d)", pos, want, x, y)
		}
	}
	// 工作区不在原点（任务栏在顶部或左侧）
	if x, y := PopupOrigin("top_left", IRect{0, 40, 1920, 1040}, 400, 184, 20); x != 20 || y != 60 {
		t.Errorf("应避开顶部任务栏，实际 (%d,%d)", x, y)
	}
}

func TestNormalizePosition(t *testing.T) {
	if p, ok := NormalizePosition("corner"); !ok || p != "bottom_right" {
		t.Error("v1 的 corner 应迁移为 bottom_right")
	}
	for _, v := range PositionOrder {
		if p, ok := NormalizePosition(v); !ok || p != v {
			t.Errorf("%s 应原样通过", v)
		}
	}
	if _, ok := NormalizePosition("foo"); ok {
		t.Error("未知位置应被拒绝")
	}
}

func center(r IRect) (int, int) { return r.X + r.W/2, r.Y + r.H/2 }

func TestPopupLayoutHits(t *testing.T) {
	for _, dpi := range []int{96, 120, 144, 192} {
		s := Scale{dpi}
		l := NewPopupLayout(s, false)
		for i, r := range l.Hits {
			if r.W == 0 {
				t.Fatalf("dpi=%d：真实弹窗的区域 %d 应可点击", dpi, i)
			}
			x, y := center(r)
			if got := l.HitTest(x, y); got != i {
				t.Errorf("dpi=%d：区域 %d 的中心应命中自己，实际 %d", dpi, i, got)
			}
			if r.X < 0 || r.Y < 0 || r.X+r.W > l.W || r.Y+r.H > l.H {
				t.Errorf("dpi=%d：区域 %d 超出窗口", dpi, i)
			}
		}
		if got := l.HitTest(l.Card.X+5, l.Card.Y+5); got != PopHitNone {
			t.Errorf("dpi=%d：卡片空白处不应命中按钮", dpi)
		}
		if NewPopupLayout(s, true).Hits[PopHitLink].W != 0 {
			t.Errorf("dpi=%d：预览模式不应有“设置”链接", dpi)
		}
	}
	// 150% 缩放：窗口 = (400+64)×1.5
	if l := NewPopupLayout(Scale{144}, false); l.W != 696 || l.H != 372 {
		t.Errorf("144 DPI 窗口尺寸应为 696×372，实际 %d×%d", l.W, l.H)
	}
}

func intersects(a, b FRect) bool {
	return a.X < b.Right() && b.X < a.Right() && a.Y < b.Bottom() && b.Y < a.Bottom()
}

func TestSettingsLayoutRegions(t *testing.T) {
	for _, dpi := range []int{96, 120, 144, 192} {
		l := NewSettingsLayout(Scale{dpi})
		for i, r := range l.Regions {
			if r.W <= 0 || r.H <= 0 {
				t.Fatalf("dpi=%d：区域 %d 为空", dpi, i)
			}
			if r.X < 0 || r.Y < 0 || r.Right() > float64(l.W) || r.Bottom() > float64(l.H) {
				t.Errorf("dpi=%d：区域 %d 超出窗口", dpi, i)
			}
			if got := l.HitTest(int(r.X+r.W/2), int(r.Y+r.H/2)); got != i {
				t.Errorf("dpi=%d：区域 %d 的中心应命中自己，实际 %d", dpi, i, got)
			}
			for j := i + 1; j < HitCount; j++ {
				if intersects(r, l.Regions[j]) {
					t.Errorf("dpi=%d：区域 %d 与 %d 重叠", dpi, i, j)
				}
			}
		}
		for i, e := range l.Edits {
			if e.Right() > float64(l.W) || e.Bottom() > float64(l.H) {
				t.Errorf("dpi=%d：输入框 %d 超出窗口", dpi, i)
			}
			for j, r := range l.Regions {
				if intersects(e, r) {
					t.Errorf("dpi=%d：输入框 %d 与可点击区域 %d 重叠（点击会被抢走）", dpi, i, j)
				}
			}
		}
	}
}

func inside(r IRect, w, h int) bool { return r.X >= 0 && r.Y >= 0 && r.X+r.W <= w && r.Y+r.H <= h }

func TestDrawPopupRuns(t *testing.T) {
	s := Scale{96}
	l := NewPopupLayout(s, false)
	cv := NewCanvas(l.W, l.H)

	realRuns := DrawPopup(cv, s, PopupSpec{Minutes: 50, SnoozeMin: 5, Hover: PopHitNone})
	var all []string
	for _, r := range realRuns {
		all = append(all, r.Text)
		if !inside(r.R, l.W, l.H) {
			t.Errorf("文字 %q 超出窗口", r.Text)
		}
	}
	joined := strings.Join(all, "|")
	for _, want := range []string{"该起来活动一下了", "已连续使用电脑 50 分钟", "5 分钟后提醒", "设置"} {
		if !strings.Contains(joined, want) {
			t.Errorf("真实弹窗缺少文案 %q", want)
		}
	}

	cv = NewCanvas(l.W, l.H)
	prev := DrawPopup(cv, s, PopupSpec{SnoozeMin: 5, Preview: true, Hover: PopHitNone})
	all = nil
	for _, r := range prev {
		all = append(all, r.Text)
	}
	joined = strings.Join(all, "|")
	if !strings.Contains(joined, "预览") || strings.Contains(joined, "设置") || strings.Contains(joined, "已连续使用电脑") {
		t.Errorf("预览弹窗应显示“预览”标记，且不出现“设置”和真实用时：%s", joined)
	}
}

func TestDrawPopupTransparentOutsideShadow(t *testing.T) {
	s := Scale{96}
	l := NewPopupLayout(s, false)
	cv := NewCanvas(l.W, l.H)
	DrawPopup(cv, s, PopupSpec{Minutes: 50, SnoozeMin: 5, Hover: PopHitNone})
	if a := alphaAt(cv, 0, 0); a != 0 {
		t.Errorf("窗口角落应完全透明（否则会出现方形底色），alpha=%d", a)
	}
	cx, cy := l.Card.X+l.Card.W/2, l.Card.Y+l.Card.H-4
	if a := alphaAt(cv, cx, cy); a != 255 {
		t.Errorf("卡片内部应不透明，alpha=%d", a)
	}
	if a := alphaAt(cv, cx, l.Card.Y+l.Card.H+6); a == 0 || a == 255 {
		t.Errorf("卡片下方应有半透明阴影，alpha=%d", a)
	}
}

func TestDrawSettingsRuns(t *testing.T) {
	s := Scale{96}
	l := NewSettingsLayout(s)
	cv := NewCanvas(l.W, l.H)
	st := SettingsState{Pos: "top_right", Sound: true, Hover: HitNone, Focus: -1}
	runs := DrawSettings(cv, s, st)
	var all []string
	for _, r := range runs {
		all = append(all, r.Text)
		if !inside(r.R, l.W, l.H) {
			t.Errorf("文字 %q 超出窗口", r.Text)
		}
	}
	joined := strings.Join(all, "|")
	for _, want := range []string{"久坐提醒", "提醒规则", "右上角", "预览效果", "保存", "取消", "退出程序", "开机自启", "任务栏图标", "提示音", "暂停提醒"} {
		if !strings.Contains(joined, want) {
			t.Errorf("设置窗口缺少文案 %q", want)
		}
	}
	if a := alphaAt(cv, 3, 3); a != 255 {
		t.Errorf("设置窗口背景应不透明，alpha=%d", a)
	}
	if strings.Contains(joined, "出错了") {
		t.Error("无错误时不应显示错误文字")
	}
	st.Err = "出错了"
	runs = DrawSettings(NewCanvas(l.W, l.H), s, st)
	if last := runs[len(runs)-1]; last.Text != "出错了" || last.Color != colDanger {
		t.Error("有错误时应在末尾追加红色错误文字")
	}
}

// TestWritePreviews 把界面渲染成 PNG 以便人工检查。
// 仅当设置了环境变量 STANDUP_PREVIEW_DIR 时才会写文件，普通 go test 不产生任何副作用。
// 同时导出文字块的 JSON，供外部工具叠加真实字体渲染。
func TestWritePreviews(t *testing.T) {
	dir := os.Getenv("STANDUP_PREVIEW_DIR")
	if dir == "" {
		t.Skip("未设置 STANDUP_PREVIEW_DIR，跳过预览输出")
	}
	write := func(name string, cv *Canvas, runs []TextRun) {
		f, err := os.Create(filepath.Join(dir, name+".png"))
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		if err := png.Encode(f, cv.ToImage()); err != nil {
			t.Fatal(err)
		}
		b, _ := json.MarshalIndent(runs, "", " ")
		if err := os.WriteFile(filepath.Join(dir, name+".json"), b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, dpi := range []int{96, 144} {
		s := Scale{dpi}
		suffix := map[int]string{96: "", 144: "@150"}[dpi]

		l := NewPopupLayout(s, false)
		cv := NewCanvas(l.W, l.H)
		cv.Clear(RGB(0xC9, 0xD3, 0xDB)) // 模拟桌面底色，便于观察阴影
		write("popup"+suffix, cv, DrawPopup(cv, s, PopupSpec{Minutes: 50, SnoozeMin: 5, Hover: PopHitNone}))

		cv = NewCanvas(l.W, l.H)
		cv.Clear(RGB(0xC9, 0xD3, 0xDB))
		write("popup_hover"+suffix, cv, DrawPopup(cv, s, PopupSpec{Minutes: 50, SnoozeMin: 5, Hover: PopHitPrimary}))

		lp := NewPopupLayout(s, true)
		cv = NewCanvas(lp.W, lp.H)
		cv.Clear(RGB(0xC9, 0xD3, 0xDB))
		write("popup_preview"+suffix, cv, DrawPopup(cv, s, PopupSpec{SnoozeMin: 5, Preview: true, Hover: PopHitNone}))

		sl := NewSettingsLayout(s)
		cv = NewCanvas(sl.W, sl.H)
		write("settings"+suffix, cv, DrawSettings(cv, s, SettingsState{Pos: "bottom_right", Sound: true, Tray: true, Hover: HitPosBase + 1, Focus: 0}))
	}
}

func TestDrawSettingsPaused(t *testing.T) {
	s := Scale{96}
	l := NewSettingsLayout(s)
	st := SettingsState{Pos: "bottom_right", Hover: HitNone, Focus: -1, Paused: true, PauseNote: "已暂停，15:30 自动恢复"}
	var all []string
	for _, r := range DrawSettings(NewCanvas(l.W, l.H), s, st) {
		all = append(all, r.Text)
	}
	joined := strings.Join(all, "|")
	if !strings.Contains(joined, "开启提醒") || strings.Contains(joined, "暂停提醒") {
		t.Errorf("已暂停时按钮应为“开启提醒”：%s", joined)
	}
	if !strings.Contains(joined, "已暂停，15:30 自动恢复") || strings.Contains(joined, "连续使用满设定时间后") {
		t.Errorf("已暂停时页头应显示暂停说明：%s", joined)
	}
}
