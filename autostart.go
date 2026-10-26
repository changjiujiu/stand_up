package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

const (
	startupFileName = "StandUp-Reminder.cmd"
	startupMarker   = "rem managed-by: standup-reminder" // 只认带此标记的文件，绝不动别人的文件
)

func startupPath() (string, error) {
	appData := os.Getenv("APPDATA")
	if appData == "" {
		return "", errors.New("未找到 APPDATA 环境变量")
	}
	return filepath.Join(appData, "Microsoft", "Windows", "Start Menu", "Programs", "Startup", startupFileName), nil
}

// SyncAutostart 让“启动”文件夹的状态与配置一致；返回非空字符串表示发生了变更
func SyncAutostart(enable bool, exePath string) (string, error) {
	path, err := startupPath()
	if err != nil {
		return "", err
	}
	old, readErr := os.ReadFile(path)

	if enable {
		content := "@echo off\r\n" + startupMarker + "\r\nstart \"\" \"" + exePath + "\"\r\n"
		if readErr == nil && string(old) == content {
			return "", nil // 已是最新，无需改动
		}
		if readErr == nil && !strings.Contains(string(old), startupMarker) {
			return "", errors.New("启动文件夹中已有同名文件且不是本程序创建的，未覆盖：" + path)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			return "", err
		}
		return "已开启开机自启：" + path, nil
	}

	// 关闭：只删除本程序创建的文件
	if errors.Is(readErr, os.ErrNotExist) || !strings.Contains(string(old), startupMarker) {
		return "", nil
	}
	if err := os.Remove(path); err != nil {
		return "", err
	}
	return "已关闭开机自启，删除：" + path, nil
}
