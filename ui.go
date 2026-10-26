package main

// 与平台无关的界面布局与绘制：弹窗、设置窗口。
// 形状由 Canvas 抗锯齿绘制，文字以 TextRun 形式返回，由平台层用系统字体渲染。
// 这样布局和命中测试都可以在任何平台上单测，也能直接输出预览图。

import (
	"fmt"
	"math"
)

// ---------------- 基础类型 ----------------

// Scale 把 96 DPI 下的逻辑像素换算为物理像素
type Scale struct{ DPI int }

func (s Scale) F(v float64) float64 {
	d := s.DPI
	if d <= 0 {
		d = 96
	}
	return v * float64(d) / 96
}

type FRect struct{ X, Y, W, H float64 }

func (r FRect) Right() float64  { return r.X + r.W }
func (r FRect) Bottom() float64 { return r.Y + r.H }
func (r FRect) Contains(x, y float64) bool {
	return x >= r.X && x < r.X+r.W && y >= r.Y && y < r.Y+r.H
}

// Inset 向内缩进 d（d 为负时向外扩）
func (r FRect) Inset(d float64) FRect { return FRect{r.X + d, r.Y + d, r.W - 2*d, r.H - 2*d} }

type IRect struct{ X, Y, W, H int }

func (r IRect) Contains(x, y int) bool { return x >= r.X && x < r.X+r.W && y >= r.Y && y < r.Y+r.H }

// I 四边分别四舍五入，保证相邻矩形取整后不留缝
func (r FRect) I() IRect {
	x0, y0 := int(math.Round(r.X)), int(math.Round(r.Y))
	return IRect{x0, y0, int(math.Round(r.Right())) - x0, int(math.Round(r.Bottom())) - y0}
}

// FontID 字体槽位，由平台层映射为具体字体
type FontID int

const (
	FontTitle   FontID = iota // 20px 粗体
	FontBody                  // 14px
	FontButton                // 14px 半粗
	FontSmall                 // 13px
	FontSection               // 15px 粗体
	FontCaption               // 12px 半粗
	FontCount
)

type Align int

const (
	AlignLeft Align = iota
	AlignCenter
	AlignRight
)

// TextRun 一段待绘制的文字；坐标为物理像素
type TextRun struct {
	Text  string
	R     IRect
	Font  FontID
	Color Color
	Align Align
	Wrap  bool // true：多行自动换行（顶对齐）；false：单行垂直居中
}

func run(text string, font FontID, col Color, r FRect, al Align, wrap bool) TextRun {
	return TextRun{Text: text, R: r.I(), Font: font, Color: col, Align: al, Wrap: wrap}
}

// ---------------- 配色 ----------------

var (
	colWindow      = RGB(0xF3, 0xF5, 0xF7)
	colCard        = RGB(0xFF, 0xFF, 0xFF)
	colLine        = RGB(0xEC, 0xEF, 0xF1)
	colBorder      = RGB(0xE3, 0xE6, 0xE8)
	colInputBorder = RGB(0xD3, 0xD9, 0xDF)
	colAccent      = RGB(0x1F, 0x8A, 0x70)
	colAccentHover = RGB(0x17, 0x6E, 0x59)
	colAccentTint  = RGB(0xE4, 0xF3, 0xEE)
	colSecondary   = RGB(0xF1, 0xF3, 0xF4)
	colSecondaryHv = RGB(0xE2, 0xE6, 0xE8)
	colTitle       = RGB(0x1F, 0x23, 0x28)
	colBody        = RGB(0x5A, 0x62, 0x6B)
	colMuted       = RGB(0x8A, 0x93, 0x9C)
	colWhite       = RGB(0xFF, 0xFF, 0xFF)
	colOnSecondary = RGB(0x33, 0x3A, 0x40)
	colDanger      = RGB(0xC6, 0x28, 0x28)
	colToggleOff   = RGB(0xC5, 0xCB, 0xD1)
	colMonitor     = RGB(0xDD, 0xE2, 0xE7)
	colScreen      = RGB(0xF8, 0xFA, 0xFB)
	colPadIdle     = RGB(0xD9, 0xDF, 0xE4)
	colPadHover    = RGB(0xBC, 0xC6, 0xCE)
)

// ---------------- 弹窗位置 ----------------

// PositionOrder 五个可选位置；下标同时是设置窗口示意图热区的编号
var PositionOrder = [5]string{"top_left", "top_right", "bottom_left", "bottom_right", "center"}

var positionLabels = map[string]string{
	"top_left": "左上角", "top_right": "右上角", "bottom_left": "左下角", "bottom_right": "右下角", "center": "屏幕中央",
}

func PositionLabel(pos string) string {
	if l, ok := positionLabels[pos]; ok {
		return l
	}
	return positionLabels["bottom_right"]
}

// NormalizePosition 校验位置名；兼容 v1 配置里的 corner
func NormalizePosition(v string) (string, bool) {
	if v == "corner" {
		return "bottom_right", true
	}
	if _, ok := positionLabels[v]; ok {
		return v, true
	}
	return "", false
}

// PopupOrigin 计算卡片左上角：wa 为工作区，margin 为距屏幕边缘的留白
func PopupOrigin(pos string, wa IRect, cardW, cardH, margin int) (x, y int) {
	left, right := wa.X+margin, wa.X+wa.W-cardW-margin
	top, bottom := wa.Y+margin, wa.Y+wa.H-cardH-margin
	switch pos {
	case "top_left":
		return left, top
	case "top_right":
		return right, top
	case "bottom_left":
		return left, bottom
	case "center":
		return wa.X + (wa.W-cardW)/2, wa.Y + (wa.H-cardH)/2
	}
	return right, bottom // bottom_right 及未知值
}

// ---------------- 提醒弹窗 ----------------

const (
	popupCardW  = 400.0
	popupCardH  = 184.0
	PopupMargin = 32.0 // 卡片四周为阴影预留的透明边（逻辑像素）
)

// 弹窗可点击区域编号
const (
	PopHitNone      = -1
	PopHitPrimary   = 0 // 好，去活动
	PopHitSecondary = 1 // N 分钟后提醒
	PopHitLink      = 2 // 右上角“设置”（预览模式下没有）
)

type PopupSpec struct {
	Minutes   int  // 已连续使用分钟数
	SnoozeMin int  // “稍后提醒”的分钟数
	Preview   bool // 预览模式：换成示例文案并显示“预览”标记
	Hover     int  // 当前悬停的区域，PopHitNone 表示无
}

// PopupLayout 弹窗窗口尺寸与命中区域（物理像素，窗口坐标）
type PopupLayout struct {
	W, H int
	Card IRect
	Hits [3]IRect // 零值表示该区域不可点击
}

type popupGeometry struct {
	win                [2]int
	card, badge        FRect
	title, body, link  FRect
	primary, secondary FRect
}

func popupGeom(s Scale) popupGeometry {
	m, cw, ch := s.F(PopupMargin), s.F(popupCardW), s.F(popupCardH)
	at := func(x, y, w, h float64) FRect { return FRect{m + s.F(x), m + s.F(y), s.F(w), s.F(h)} }
	return popupGeometry{
		win:       [2]int{int(math.Round(cw + 2*m)), int(math.Round(ch + 2*m))},
		card:      FRect{m, m, cw, ch},
		badge:     at(24, 26, 48, 48),
		title:     at(88, 24, 232, 28),
		body:      at(88, 56, 288, 56),
		link:      at(320, 24, 56, 24),
		primary:   at(252, 120, 124, 40),
		secondary: at(118, 120, 124, 40),
	}
}

func NewPopupLayout(s Scale, preview bool) PopupLayout {
	g := popupGeom(s)
	l := PopupLayout{W: g.win[0], H: g.win[1], Card: g.card.I()}
	l.Hits[PopHitPrimary] = g.primary.I()
	l.Hits[PopHitSecondary] = g.secondary.I()
	if !preview {
		l.Hits[PopHitLink] = g.link.Inset(s.F(-4)).I()
	}
	return l
}

// HitTest 返回命中的区域编号
func (l PopupLayout) HitTest(x, y int) int {
	for i, r := range l.Hits {
		if r.Contains(x, y) {
			return i
		}
	}
	return PopHitNone
}

// DrawPopup 在透明画布上绘制弹窗形状，返回需要叠加的文字
func DrawPopup(cv *Canvas, s Scale, spec PopupSpec) []TextRun {
	g := popupGeom(s)
	c := g.card

	// 软阴影：环境阴影 + 接触阴影，叠出层次
	cv.Shadow(c.X, c.Y, c.W, c.H, s.F(16), s.F(7), s.F(9), Color{0, 0, 0, 62})
	cv.Shadow(c.X, c.Y, c.W, c.H, s.F(16), s.F(1), s.F(2.5), Color{0, 0, 0, 38})
	cv.FillRoundRect(c.X, c.Y, c.W, c.H, s.F(16), colCard)
	cv.StrokeRoundRect(c.X, c.Y, c.W, c.H, s.F(16), s.F(1), colBorder)

	// 图标徽章
	b := g.badge
	cv.FillRoundRect(b.X, b.Y, b.W, b.H, s.F(14), colAccentTint)
	cv.DrawFigure(b.X+b.W*0.12, b.Y+b.H*0.12, b.W*0.76, colAccent)

	// 按钮
	btn := func(r FRect, bg Color) { cv.FillRoundRect(r.X, r.Y, r.W, r.H, s.F(12), bg) }
	prim, sec := colAccent, colSecondary
	if spec.Hover == PopHitPrimary {
		prim = colAccentHover
	}
	if spec.Hover == PopHitSecondary {
		sec = colSecondaryHv
	}
	btn(g.primary, prim)
	btn(g.secondary, sec)

	body := fmt.Sprintf("你已连续使用电脑 %d 分钟。\n站起来走走，喝口水，看看远处。", spec.Minutes)
	runs := []TextRun{
		run("该起来活动一下了", FontTitle, colTitle, g.title, AlignLeft, false),
		run("好，去活动", FontButton, colWhite, g.primary, AlignCenter, false),
		run(fmt.Sprintf("%d 分钟后提醒", spec.SnoozeMin), FontButton, colOnSecondary, g.secondary, AlignCenter, false),
	}
	if spec.Preview {
		body = "这是弹窗的预览效果，不影响计时。\n实际提醒会显示已连续使用的分钟数。"
		cv.FillRoundRect(g.link.X, g.link.Y, g.link.W, g.link.H, g.link.H/2, colAccentTint)
		runs = append(runs, run("预览", FontCaption, colAccent, g.link, AlignCenter, false))
	} else {
		linkCol := colMuted
		if spec.Hover == PopHitLink {
			linkCol = colAccent
		}
		runs = append(runs, run("设置", FontSmall, linkCol, g.link, AlignRight, false))
	}
	return append(runs, run(body, FontBody, colBody, g.body, AlignLeft, true))
}

// ---------------- 设置窗口 ----------------

const (
	settingsW = 700.0
	settingsH = 484.0
)

// 设置窗口可点击区域编号：0-4 为位置热区（顺序同 PositionOrder）
const (
	HitNone    = -1
	HitPosBase = 0
	HitSound   = 5
	HitAuto    = 6
	HitTray    = 7
	HitPreview = 8
	HitSave    = 9
	HitCancel  = 10
	HitQuit    = 11
	HitCount   = 12
)

type SettingsState struct {
	Pos               string
	Sound, Auto, Tray bool
	Hover             int // HitXxx
	Focus             int // 获得焦点的输入框下标，-1 表示无
	Err               string
}

// SettingsLayout 设置窗口布局（物理像素浮点矩形）
type SettingsLayout struct {
	W, H    int
	Edits   [3]FRect        // 输入框外框；EDIT 控件嵌在其内部
	Regions [HitCount]FRect // 全部可点击区域
}

// 逻辑坐标常量：左右两栏
const (
	colLX, colRX, colW = 24.0, 358.0, 318.0
	cardAY, cardAH     = 116.0, 144.0 // 提醒规则
	cardCY, cardCH     = 300.0, 100.0 // 系统
	cardBY, cardBH     = 116.0, 284.0 // 弹窗
	rowH               = 44.0
)

func NewSettingsLayout(s Scale) SettingsLayout {
	f := func(x, y, w, h float64) FRect { return FRect{s.F(x), s.F(y), s.F(w), s.F(h)} }
	l := SettingsLayout{W: int(math.Round(s.F(settingsW))), H: int(math.Round(s.F(settingsH)))}

	// 提醒规则：三个输入框
	for i := range l.Edits {
		l.Edits[i] = f(206, cardAY+6+rowH*float64(i)+6, 76, 32)
	}

	// 弹窗位置示意图：显示器 236×132，屏幕内缩 6，五个热区
	mon := f(colRX+(colW-236)/2, 132, 236, 132)
	scr := mon.Inset(s.F(6))
	pw, ph, in := s.F(52), s.F(30), s.F(12)
	pads := [5]FRect{
		{scr.X + in, scr.Y + in, pw, ph},
		{scr.Right() - in - pw, scr.Y + in, pw, ph},
		{scr.X + in, scr.Bottom() - in - ph, pw, ph},
		{scr.Right() - in - pw, scr.Bottom() - in - ph, pw, ph},
		{scr.X + (scr.W-pw)/2, scr.Y + (scr.H-ph)/2, pw, ph},
	}
	for i, p := range pads {
		l.Regions[HitPosBase+i] = p.Inset(s.F(-6))
	}

	// 开关行：点整行都可切换
	l.Regions[HitSound] = f(colRX+8, 340, colW-16, rowH)
	l.Regions[HitAuto] = f(colLX+8, cardCY+6, colW-16, rowH)
	l.Regions[HitTray] = f(colLX+8, cardCY+6+rowH, colW-16, rowH)

	// 按钮
	l.Regions[HitPreview] = f(colRX+colW-16-104, 294, 104, 36)
	l.Regions[HitSave] = f(settingsW-24-104, 424, 104, 40)
	l.Regions[HitCancel] = f(settingsW-24-104-12-96, 424, 96, 40)
	l.Regions[HitQuit] = f(24, 424, 88, 40)
	return l
}

// HitTest 返回命中的区域编号
func (l SettingsLayout) HitTest(x, y int) int {
	for i, r := range l.Regions {
		if r.Contains(float64(x), float64(y)) {
			return i
		}
	}
	return HitNone
}

func drawToggle(cv *Canvas, s Scale, row FRect, on, hover bool) {
	w, h := s.F(40), s.F(22)
	x, y := row.Right()-s.F(8)-w, row.Y+(row.H-h)/2
	track := colToggleOff
	if on {
		track = colAccent
		if hover {
			track = colAccentHover
		}
	} else if hover {
		track = colPadHover
	}
	cv.FillRoundRect(x, y, w, h, h/2, track)
	d := s.F(16)
	kx := x + s.F(3)
	if on {
		kx = x + w - s.F(3) - d
	}
	cv.Shadow(kx, y+s.F(3), d, d, d/2, s.F(1), s.F(1.5), Color{0, 0, 0, 40})
	cv.FillCircle(kx+d/2, y+h/2, d/2, colWhite)
}

func drawButton(cv *Canvas, s Scale, r FRect, bg Color) {
	cv.FillRoundRect(r.X, r.Y, r.W, r.H, s.F(10), bg)
}

// DrawSettings 绘制整个设置窗口（不透明），返回需要叠加的文字
func DrawSettings(cv *Canvas, s Scale, st SettingsState) []TextRun {
	l := NewSettingsLayout(s)
	f := func(x, y, w, h float64) FRect { return FRect{s.F(x), s.F(y), s.F(w), s.F(h)} }
	cv.Clear(colWindow)
	var runs []TextRun

	// 页头
	cv.FillRoundRect(s.F(24), s.F(20), s.F(44), s.F(44), s.F(12), colAccent)
	cv.DrawFigure(s.F(24+7), s.F(20+7), s.F(30), colWhite)
	runs = append(runs,
		run("久坐提醒", FontTitle, colTitle, f(80, 18, 300, 28), AlignLeft, false),
		run("连续使用满设定时间后，提醒你起身活动", FontSmall, colMuted, f(80, 46, 400, 20), AlignLeft, false))

	// 三张卡片 + 小标题
	card := func(x, y, w, h float64, title string) {
		cv.FillRoundRect(s.F(x), s.F(y), s.F(w), s.F(h), s.F(14), colCard)
		cv.StrokeRoundRect(s.F(x), s.F(y), s.F(w), s.F(h), s.F(14), s.F(1), colBorder)
		runs = append(runs, run(title, FontCaption, colMuted, f(x+4, y-24, w, 20), AlignLeft, false))
	}
	card(colLX, cardAY, colW, cardAH, "提醒规则")
	card(colLX, cardCY, colW, cardCH, "系统")
	card(colRX, cardBY, colW, cardBH, "弹窗")
	sep := func(x, y, w float64) { cv.FillRoundRect(s.F(x), s.F(y), s.F(w), s.F(1), 0, colLine) }

	// 提醒规则：三行输入
	labels := [3]string{"连续使用多久后提醒", "离开多久算已休息", "稍后提醒的间隔"}
	for i, e := range l.Edits {
		top := cardAY + 6 + rowH*float64(i)
		runs = append(runs,
			run(labels[i], FontBody, colTitle, f(40, top, 160, rowH), AlignLeft, false),
			run("分钟", FontSmall, colBody, f(290, top, 40, rowH), AlignLeft, false))
		border, t := colInputBorder, 1.0
		if st.Focus == i {
			border, t = colAccent, 1.5
		}
		cv.FillRoundRect(e.X, e.Y, e.W, e.H, s.F(8), colWhite)
		cv.StrokeRoundRect(e.X, e.Y, e.W, e.H, s.F(8), s.F(t), border)
		if i < 2 {
			sep(40, top+rowH, 286)
		}
	}

	// 系统：两个开关
	sysRows := [2]struct {
		label string
		hit   int
		on    bool
	}{{"开机自启", HitAuto, st.Auto}, {"任务栏图标", HitTray, st.Tray}}
	for i, r := range sysRows {
		row := l.Regions[r.hit]
		runs = append(runs, run(r.label, FontBody, colTitle, FRect{s.F(40), row.Y, s.F(160), row.H}, AlignLeft, false))
		drawToggle(cv, s, row, r.on, st.Hover == r.hit)
		if i == 0 {
			sep(40, cardCY+6+rowH, 286)
		}
	}

	// 弹窗：示意图
	mon := f(colRX+(colW-236)/2, 132, 236, 132)
	cx := mon.X + mon.W/2
	cv.FillRoundRect(cx-s.F(12), mon.Bottom()-s.F(2), s.F(24), s.F(8), 0, colMonitor)
	cv.FillRoundRect(cx-s.F(34), mon.Bottom()+s.F(4), s.F(68), s.F(5), s.F(2.5), colMonitor)
	cv.FillRoundRect(mon.X, mon.Y, mon.W, mon.H, s.F(10), colMonitor)
	scr := mon.Inset(s.F(6))
	cv.FillRoundRect(scr.X, scr.Y, scr.W, scr.H, s.F(6), colScreen)
	for i, name := range PositionOrder {
		pad := l.Regions[HitPosBase+i].Inset(s.F(6))
		col := colPadIdle
		switch {
		case name == st.Pos:
			col = colAccent
		case st.Hover == HitPosBase+i:
			col = colPadHover
		}
		cv.FillRoundRect(pad.X, pad.Y, pad.W, pad.H, s.F(6), col)
		if name == st.Pos { // 选中项画两条白线，暗示这是一张弹窗卡片
			cv.FillRoundRect(pad.X+s.F(7), pad.Y+s.F(8), pad.W*0.5, s.F(4), s.F(2), colWhite)
			cv.FillRoundRect(pad.X+s.F(7), pad.Y+s.F(16), pad.W-s.F(14), s.F(4), s.F(2), colWhite.WithAlpha(190))
		}
	}
	sep(colRX+16, 284, colW-32)
	runs = append(runs,
		run("弹窗位置", FontBody, colTitle, f(colRX+16, 288, 72, 48), AlignLeft, false),
		run(PositionLabel(st.Pos), FontButton, colAccent, f(colRX+16+76, 288, 80, 48), AlignLeft, false))
	pv := l.Regions[HitPreview]
	pvBg := colSecondary
	if st.Hover == HitPreview {
		pvBg = colSecondaryHv
	}
	drawButton(cv, s, pv, pvBg)
	runs = append(runs, run("预览效果", FontButton, colOnSecondary, pv, AlignCenter, false))
	sep(colRX+16, 336, colW-32)
	snd := l.Regions[HitSound]
	runs = append(runs, run("提示音", FontBody, colTitle, FRect{s.F(colRX + 16), snd.Y, s.F(160), snd.H}, AlignLeft, false))
	drawToggle(cv, s, snd, st.Sound, st.Hover == HitSound)

	// 页脚
	save, cancel, quit := l.Regions[HitSave], l.Regions[HitCancel], l.Regions[HitQuit]
	saveBg, cancelBg, quitCol := colAccent, colSecondary, colMuted
	if st.Hover == HitSave {
		saveBg = colAccentHover
	}
	if st.Hover == HitCancel {
		cancelBg = colSecondaryHv
	}
	if st.Hover == HitQuit {
		quitCol = colDanger
	}
	drawButton(cv, s, save, saveBg)
	drawButton(cv, s, cancel, cancelBg)
	runs = append(runs,
		run("保存", FontButton, colWhite, save, AlignCenter, false),
		run("取消", FontButton, colOnSecondary, cancel, AlignCenter, false),
		run("退出程序", FontBody, quitCol, quit, AlignLeft, false))
	if st.Err != "" {
		runs = append(runs, run(st.Err, FontSmall, colDanger, f(120, 424, 330, 40), AlignLeft, false))
	}
	return runs
}
