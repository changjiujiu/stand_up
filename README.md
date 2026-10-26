# stand_up

Windows 久坐提醒：按键鼠活动计时，连续使用满 N 分钟后弹窗提醒起来活动。

- 只用 Go 标准库 + Windows 自带 DLL，零第三方依赖，单文件 exe，免安装
- 以键鼠空闲时长判断是否离开：离开满 M 分钟自动清零，睡眠 / 锁屏恢复后不会误弹
- 提醒弹窗不抢键盘焦点，可拖动；支持“稍后提醒”
- 图形化设置窗口，保存后立即生效；任务栏托盘图标（左键设置，右键菜单）
- 开机自启：在“启动”文件夹写入 `StandUp-Reminder.cmd`，只增删自己创建的文件，不改注册表

## 使用

1. 运行 `StandUp.exe`，首次运行自动打开设置窗口，并在 exe 同目录生成 `standup.ini`
2. 再次运行 exe，或单击托盘图标，即可回到设置窗口；窗口左下角可退出程序

| 设置项 | 默认值 | 说明 |
|---|---|---|
| 连续使用多久后提醒 | 50 分钟 | 1–600 |
| 离开多久算已休息 | 5 分钟 | 1–120，计时清零 |
| 稍后提醒的间隔 | 5 分钟 | 1–120 |
| 弹窗位置 | 右下角 | 右下角 / 屏幕中央 |
| 提示音 | 开 | |
| 开机自启 | 关 | |
| 任务栏图标 | 开 | 关闭后再次运行 exe 仍可打开设置 |

## 编译

需要 Go 1.24+。图标、清单和版本信息已预生成为 `rsrc_windows_amd64.syso`，会被自动链接，可在 Linux / macOS / Windows 上交叉编译：

```bash
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 \
  go build -trimpath -ldflags "-s -w -H windowsgui" -o StandUp.exe .
```

状态机与配置逻辑与平台无关，可直接测试：`go test ./...`

如需修改图标或版本信息，编辑 `winres/winres.json` 后用 [go-winres](https://github.com/tc-hib/go-winres) 重新生成：`go-winres make --in winres/winres.json --arch amd64`（仅构建期使用，不进入运行时）。

## 说明

exe 未做代码签名，在启用应用白名单或杀软的公司终端上可能被拦截；建议自行审阅源码后本地编译。
