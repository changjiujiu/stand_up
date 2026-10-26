package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// 各分钟参数的合法范围（配置文件与设置界面共用）
const (
	workMin, workMax     = 1, 600
	idleMin, idleMax     = 1, 120
	snoozeMin, snoozeMax = 1, 120
)

// iniTemplate 配置文件模板（UTF-8；一般通过设置界面修改，也可用记事本编辑）
const iniTemplate = `; 久坐提醒 配置文件
; 推荐通过设置界面修改（左键单击托盘图标，或再次运行 StandUp.exe），保存后立即生效
; 手工编辑本文件后需重启程序才生效

[reminder]
; 连续使用多少分钟后提醒（%d-%d）
work_minutes = %d

; 无键鼠操作多少分钟视为已休息，计时清零（%d-%d）
idle_reset_minutes = %d

; 点“稍后提醒”后延迟多少分钟（%d-%d）
snooze_minutes = %d

; 弹窗位置：top_left = 左上角，top_right = 右上角，bottom_left = 左下角，
;           bottom_right = 右下角，center = 屏幕中央
position = %s

; 弹出时是否提示音：true / false
sound = %t

; 任务栏托盘图标：true / false（关闭后可再次运行 exe 打开设置）
tray_icon = %t

; 开机自启：true / false
; true  时在“启动”文件夹写入 StandUp-Reminder.cmd
; false 时删除该文件（仅删除本程序创建的那一个）
autostart = %t
`

// SaveConfig 把配置完整写回 ini（CRLF 换行，方便记事本查看）
func SaveConfig(path string, c Config) error {
	s := fmt.Sprintf(iniTemplate,
		workMin, workMax, int(c.Work.Minutes()),
		idleMin, idleMax, int(c.IdleReset.Minutes()),
		snoozeMin, snoozeMax, int(c.Snooze.Minutes()),
		c.Position, c.Sound, c.Tray, c.Autostart)
	return os.WriteFile(path, []byte(strings.ReplaceAll(s, "\n", "\r\n")), 0o644)
}

// LoadConfig 读取 ini；不存在则生成默认文件。返回：配置、告警、是否新建
func LoadConfig(path string) (Config, []string, bool, error) {
	cfg := DefaultConfig()
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return cfg, nil, true, SaveConfig(path, cfg)
	}
	if err != nil {
		return cfg, nil, false, err
	}

	var warns []string
	data = bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF}) // 去掉 UTF-8 BOM
	for i, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || line[0] == ';' || line[0] == '#' || line[0] == '[' {
			continue
		}
		key, val, ok := strings.Cut(line, "=")
		if !ok {
			warns = append(warns, fmt.Sprintf("第 %d 行无法识别：%s", i+1, line))
			continue
		}
		key, val = strings.ToLower(strings.TrimSpace(key)), strings.ToLower(strings.TrimSpace(val))

		switch key {
		case "work_minutes":
			setMinutes(&cfg.Work, key, val, workMin, workMax, &warns)
		case "idle_reset_minutes":
			setMinutes(&cfg.IdleReset, key, val, idleMin, idleMax, &warns)
		case "snooze_minutes":
			setMinutes(&cfg.Snooze, key, val, snoozeMin, snoozeMax, &warns)
		case "position":
			if p, ok := NormalizePosition(val); ok { // 兼容 v1 的 corner
				cfg.Position = p
			} else {
				warns = append(warns, fmt.Sprintf("position=%s 无效，已用默认 bottom_right", val))
			}
		case "sound":
			setBool(&cfg.Sound, key, val, &warns)
		case "tray_icon":
			setBool(&cfg.Tray, key, val, &warns)
		case "autostart":
			setBool(&cfg.Autostart, key, val, &warns)
		default:
			warns = append(warns, fmt.Sprintf("未知配置项：%s", key))
		}
	}
	return cfg, warns, false, nil
}

// ParseMinutes 校验分钟数输入（设置界面用）
func ParseMinutes(name, val string, lo, hi int) (time.Duration, error) {
	n, err := strconv.Atoi(strings.TrimSpace(val))
	if err != nil || n < lo || n > hi {
		return 0, fmt.Errorf("%s 需填 %d-%d 之间的整数", name, lo, hi)
	}
	return time.Duration(n) * time.Minute, nil
}

func setMinutes(dst *time.Duration, key, val string, lo, hi int, warns *[]string) {
	d, err := ParseMinutes(key, val, lo, hi)
	if err != nil {
		*warns = append(*warns, fmt.Sprintf("%s=%s 无效（范围 %d-%d），已用默认值 %d", key, val, lo, hi, int(dst.Minutes())))
		return
	}
	*dst = d
}

func setBool(dst *bool, key, val string, warns *[]string) {
	b, err := strconv.ParseBool(val)
	if err != nil {
		*warns = append(*warns, fmt.Sprintf("%s=%s 无效（应为 true/false），已用默认值 %v", key, val, *dst))
		return
	}
	*dst = b
}
