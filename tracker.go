package main

import "time"

// Config 运行参数（由 standup.ini 加载）
type Config struct {
	Work      time.Duration // 连续活跃多久后提醒
	IdleReset time.Duration // 无键鼠操作多久视为已休息
	Snooze    time.Duration // “稍后提醒”的延迟
	Position  string        // top_left | top_right | bottom_left | bottom_right | center
	Sound     bool          // 弹出时提示音
	Autostart bool          // 开机自启
	Tray      bool          // 显示任务栏托盘图标
}

// DefaultConfig 默认参数
func DefaultConfig() Config {
	return Config{
		Work:      50 * time.Minute,
		IdleReset: 5 * time.Minute,
		Snooze:    5 * time.Minute,
		Position:  "bottom_right",
		Sound:     true,
		Tray:      true,
	}
}

// Action 一次轮询后需要 UI 执行的动作
type Action int

const (
	ActNone        Action = iota
	ActShow               // 弹出提醒
	ActAutoDismiss        // 用户已离开，自动收起提醒
)

// Tracker 计时状态机：与 Win32 解耦，便于单测
type Tracker struct {
	cfg         Config
	activeStart time.Time // 本轮连续活跃的起点
	nextDue     time.Time // 下次提醒时间
	lastTick    time.Time // 上次轮询时间（用于识别睡眠/休眠造成的空档）
	away        bool      // 当前是否处于“已离开”状态
	popup       bool      // 弹窗是否打开
	paused      bool      // 是否处于“暂停提醒”
	pauseUntil  time.Time // 暂停截止时间；零值表示直到手动恢复
}

func NewTracker(cfg Config, now time.Time) *Tracker {
	return &Tracker{cfg: cfg, activeStart: now, nextDue: now.Add(cfg.Work), lastTick: now}
}

// Tick 每次轮询调用；idle 为距上次键鼠输入的时长
func (t *Tracker) Tick(now time.Time, idle time.Duration) Action {
	if t.paused {
		t.lastTick = now // 暂停期间照常刷新，避免恢复后被误判为“睡眠空档”
		if !t.pauseUntil.IsZero() && !now.Before(t.pauseUntil) {
			t.Resume(now) // 到期自动恢复
		}
		return ActNone
	}
	// 两次轮询间隔超过阈值：说明机器睡眠/休眠过，视同离开
	gap := now.Sub(t.lastTick)
	t.lastTick = now
	if gap >= t.cfg.IdleReset {
		t.away = true
	}

	if idle >= t.cfg.IdleReset {
		t.away = true
		if t.popup {
			t.popup = false
			return ActAutoDismiss
		}
		return ActNone
	}

	// 从离开状态回来：以第一次输入的时间作为新一轮起点
	if t.away {
		t.away = false
		t.popup = false
		t.activeStart = now.Add(-idle)
		t.nextDue = t.activeStart.Add(t.cfg.Work)
		return ActAutoDismiss // 若睡眠前弹窗还开着，一并收起（无弹窗时 UI 侧为空操作）
	}

	if !t.popup && !now.Before(t.nextDue) {
		t.popup = true
		return ActShow
	}
	return ActNone
}

// Done 用户点击“去活动”：重新开始计时
func (t *Tracker) Done(now time.Time) {
	t.popup = false
	t.activeStart = now
	t.nextDue = now.Add(t.cfg.Work)
}

// Snooze 用户点击“稍后提醒”：不清零，只推迟下次提醒
func (t *Tracker) Snooze(now time.Time) {
	t.popup = false
	t.nextDue = now.Add(t.cfg.Snooze)
}

// Pause 暂停提醒；d<=0 表示直到手动恢复。暂停时若弹窗开着，调用方负责关闭窗口
func (t *Tracker) Pause(now time.Time, d time.Duration) {
	t.paused, t.popup, t.lastTick = true, false, now
	t.pauseUntil = time.Time{}
	if d > 0 {
		t.pauseUntil = now.Add(d)
	}
}

// Resume 恢复提醒并重新计满一个完整周期（演示本身也算工作时间）
func (t *Tracker) Resume(now time.Time) {
	t.paused, t.away, t.popup, t.lastTick = false, false, false, now
	t.pauseUntil = time.Time{}
	t.activeStart, t.nextDue = now, now.Add(t.cfg.Work)
}

// IsPaused 是否处于暂停状态
func (t *Tracker) IsPaused() bool { return t.paused }

// PauseUntil 暂停的截止时间；ok=false 表示没有截止时间（直到手动恢复）
func (t *Tracker) PauseUntil() (until time.Time, ok bool) {
	return t.pauseUntil, !t.pauseUntil.IsZero()
}

// UpdateConfig 设置界面保存后热更新。
// 只有提醒间隔变了才重算下次提醒时间，避免改了提示音之类的选项却把“稍后提醒”冲掉
func (t *Tracker) UpdateConfig(c Config) {
	workChanged := c.Work != t.cfg.Work
	t.cfg = c
	if workChanged && !t.popup {
		t.nextDue = t.activeStart.Add(c.Work)
	}
}

// IsAway 是否处于“已离开”状态
func (t *Tracker) IsAway() bool { return t.away }

// ActiveFor 本轮已连续活跃时长
func (t *Tracker) ActiveFor(now time.Time) time.Duration {
	return now.Sub(t.activeStart)
}
