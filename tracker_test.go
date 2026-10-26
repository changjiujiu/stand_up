package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

var t0 = time.Date(2026, 10, 23, 9, 0, 0, 0, time.Local)

func testCfg() Config {
	c := DefaultConfig()
	c.Work, c.IdleReset, c.Snooze = 50*time.Minute, 5*time.Minute, 5*time.Minute
	return c
}

// 以 15 秒步长推进，idle 恒定，返回第一次出现 want 的时间
func runUntil(tr *Tracker, from, to time.Time, idle time.Duration, want Action) (time.Time, bool) {
	for now := from; !now.After(to); now = now.Add(15 * time.Second) {
		if tr.Tick(now, idle) == want {
			return now, true
		}
	}
	return time.Time{}, false
}

func TestShowAfterWork(t *testing.T) {
	tr := NewTracker(testCfg(), t0)
	at, ok := runUntil(tr, t0.Add(15*time.Second), t0.Add(60*time.Minute), 0, ActShow)
	if !ok || at.Sub(t0) != 50*time.Minute {
		t.Fatalf("应在 50 分钟弹出，实际 %v ok=%v", at.Sub(t0), ok)
	}
	// 弹窗打开期间不重复弹出
	if _, again := runUntil(tr, at.Add(15*time.Second), at.Add(10*time.Minute), 0, ActShow); again {
		t.Fatal("弹窗打开期间重复弹出")
	}
}

func TestIdleResets(t *testing.T) {
	tr := NewTracker(testCfg(), t0)
	runUntil(tr, t0.Add(15*time.Second), t0.Add(40*time.Minute), 0, -1) // 活跃 40 分钟
	tr.Tick(t0.Add(46*time.Minute), 6*time.Minute)                      // 离开 6 分钟
	back := t0.Add(46*time.Minute + 15*time.Second)
	tr.Tick(back, 0) // 回来
	at, ok := runUntil(tr, back.Add(15*time.Second), back.Add(60*time.Minute), 0, ActShow)
	if !ok || at.Sub(back) != 50*time.Minute {
		t.Fatalf("离开后应重新计满 50 分钟，实际 %v", at.Sub(back))
	}
}

func TestShortIdleDoesNotReset(t *testing.T) {
	tr := NewTracker(testCfg(), t0)
	runUntil(tr, t0.Add(15*time.Second), t0.Add(30*time.Minute), 0, -1) // 按真实的 15 秒节奏活跃 30 分钟
	tr.Tick(t0.Add(30*time.Minute+15*time.Second), 3*time.Minute)       // 看文档 3 分钟没碰键鼠
	at, ok := runUntil(tr, t0.Add(30*time.Minute+30*time.Second), t0.Add(60*time.Minute), 0, ActShow)
	if !ok || at.Sub(t0) != 50*time.Minute {
		t.Fatalf("短暂无操作不应清零，实际 %v", at.Sub(t0))
	}
}

func TestAutoDismissWhenAway(t *testing.T) {
	tr := NewTracker(testCfg(), t0)
	at, _ := runUntil(tr, t0.Add(15*time.Second), t0.Add(60*time.Minute), 0, ActShow)
	if got := tr.Tick(at.Add(5*time.Minute), 5*time.Minute); got != ActAutoDismiss {
		t.Fatalf("人离开后应自动收起弹窗，实际 %v", got)
	}
}

func TestSnooze(t *testing.T) {
	tr := NewTracker(testCfg(), t0)
	at, _ := runUntil(tr, t0.Add(15*time.Second), t0.Add(60*time.Minute), 0, ActShow)
	tr.Snooze(at)
	again, ok := runUntil(tr, at.Add(15*time.Second), at.Add(20*time.Minute), 0, ActShow)
	if !ok || again.Sub(at) != 5*time.Minute {
		t.Fatalf("贪睡后应 5 分钟再弹，实际 %v", again.Sub(at))
	}
}

func TestDoneRestarts(t *testing.T) {
	tr := NewTracker(testCfg(), t0)
	at, _ := runUntil(tr, t0.Add(15*time.Second), t0.Add(60*time.Minute), 0, ActShow)
	tr.Done(at)
	again, ok := runUntil(tr, at.Add(15*time.Second), at.Add(60*time.Minute), 0, ActShow)
	if !ok || again.Sub(at) != 50*time.Minute {
		t.Fatalf("点去活动后应重新计 50 分钟，实际 %v", again.Sub(at))
	}
}

func TestSleepGapResets(t *testing.T) {
	tr := NewTracker(testCfg(), t0)
	tr.Tick(t0.Add(45*time.Minute), 0)
	// 睡眠 1 小时后用鼠标唤醒：idle 很小，但轮询间隔巨大
	wake := t0.Add(105 * time.Minute)
	if got := tr.Tick(wake, 10*time.Second); got == ActShow {
		t.Fatal("睡眠唤醒后不应立即弹出")
	}
	at, ok := runUntil(tr, wake.Add(15*time.Second), wake.Add(60*time.Minute), 0, ActShow)
	if !ok || at.Sub(wake) != 50*time.Minute {
		t.Fatalf("唤醒后应重新计时，实际 %v", at.Sub(wake))
	}
}

func TestLoadConfig(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "standup.ini")

	c, w, created, err := LoadConfig(p)
	if err != nil || !created || len(w) != 0 || c.Work != 50*time.Minute {
		t.Fatalf("首次应生成默认配置：%v %v %v", err, created, w)
	}
	// 生成的默认文件应能被无告警地读回
	if _, w, _, _ := LoadConfig(p); len(w) != 0 {
		t.Fatalf("默认文件读回有告警：%v", w)
	}

	os.WriteFile(p, []byte("\xEF\xBB\xBFwork_minutes = 30\r\nsnooze_minutes=abc\r\nposition=center\r\nautostart=TRUE\r\nfoo=1\r\n"), 0o644)
	c, w, _, _ = LoadConfig(p)
	if c.Work != 30*time.Minute || c.Snooze != 5*time.Minute || c.Position != "center" || !c.Autostart || len(w) != 2 {
		t.Fatalf("解析错误：%+v warns=%v", c, w)
	}
}

func TestAutostart(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("APPDATA", dir)
	sd := filepath.Join(dir, "Microsoft", "Windows", "Start Menu", "Programs", "Startup")
	os.MkdirAll(sd, 0o755)
	p := filepath.Join(sd, startupFileName)

	if n, err := SyncAutostart(true, `C:\x\standup.exe`); err != nil || n == "" {
		t.Fatalf("开启失败：%v", err)
	}
	if n, _ := SyncAutostart(true, `C:\x\standup.exe`); n != "" {
		t.Fatal("重复开启不应再写文件")
	}
	if n, err := SyncAutostart(false, ""); err != nil || n == "" {
		t.Fatalf("关闭失败：%v", err)
	}
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Fatal("关闭后文件应被删除")
	}
	// 同名但非本程序创建的文件：既不覆盖也不删除
	os.WriteFile(p, []byte("someone else"), 0o644)
	if _, err := SyncAutostart(true, `C:\x\standup.exe`); err == nil {
		t.Fatal("不应覆盖他人文件")
	}
	SyncAutostart(false, "")
	if _, err := os.Stat(p); err != nil {
		t.Fatal("不应删除他人文件")
	}
}

func TestSaveConfigRoundTrip(t *testing.T) {
	p := filepath.Join(t.TempDir(), "standup.ini")
	want := Config{Work: 25 * time.Minute, IdleReset: 3 * time.Minute, Snooze: 10 * time.Minute, Position: "center", Sound: false, Tray: false, Autostart: true}
	if err := SaveConfig(p, want); err != nil {
		t.Fatal(err)
	}
	got, w, _, err := LoadConfig(p)
	if err != nil || len(w) != 0 || got != want {
		t.Fatalf("写回再读不一致：%+v warns=%v err=%v", got, w, err)
	}
}

func TestUpdateConfig(t *testing.T) {
	tr := NewTracker(testCfg(), t0)
	at, _ := runUntil(tr, t0.Add(15*time.Second), t0.Add(60*time.Minute), 0, ActShow)
	tr.Snooze(at)
	// 只改提示音：不应冲掉“稍后提醒”
	c := testCfg()
	c.Sound = false
	tr.UpdateConfig(c)
	if again, ok := runUntil(tr, at.Add(15*time.Second), at.Add(20*time.Minute), 0, ActShow); !ok || again.Sub(at) != 5*time.Minute {
		t.Fatalf("改无关项后贪睡应保持 5 分钟，实际 %v", again.Sub(at))
	}
	// 缩短提醒间隔：按新间隔从本轮起点重算
	tr2 := NewTracker(testCfg(), t0)
	c2 := testCfg()
	c2.Work = 20 * time.Minute
	tr2.UpdateConfig(c2)
	if at2, ok := runUntil(tr2, t0.Add(15*time.Second), t0.Add(60*time.Minute), 0, ActShow); !ok || at2.Sub(t0) != 20*time.Minute {
		t.Fatalf("改间隔后应在 20 分钟弹出，实际 %v", at2.Sub(t0))
	}
}
