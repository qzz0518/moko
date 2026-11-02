# 架构

## 1. 技术栈

| 层 | 选择 |
|---|---|
| 框架 | [MyGo](https://github.com/egoist/mygo) v0.7.x，**原生 UI**（`mygo/ui`），不用网页 |
| 语言 | 仅 Go（≥1.27.3），无 cgo，运行时无 JS，项目也不需要 Bun |
| 设备通信 | ECHONET Lite，自己实现，见 [device-echonet.md](device-echonet.md) |
| Apple 家庭 | `brutella/hap` v0.0.35，见 [apple-home.md](apple-home.md) |
| 配置 | `mygo.json`（原生项目不需要 `mygo.config.ts`） |

`CGO_ENABLED=0` 下可编译到 darwin arm64/amd64 和 windows amd64/arm64。`mygo build` 产物：`.app` 15.6 MB、`.dmg` 6.2 MB、Windows `.exe` 14.6 MB、安装包 4.8 MB。

本机默认 Go 是 1.22，`go.mod` 写 `go 1.27.3` 后会自动切换工具链（已验证）；也可以用 `mise`。

### MyGo 的风险

- 项目 2026-09-26 才创建，5 周发到 v0.7.4，API 变化很快。Moniflip 用 v0.4.7，Charker 用 v0.7.1。
- **要固定版本，并按对应 tag 读文档。** 主分支文档可能超前于发布版：写样图时 `Theme.InactiveAccent` 在文档里有，v0.7.4 却编译不过。
- 缓解办法：设备和 HomeKit 逻辑不依赖 MyGo，将来换框架或加无界面版本都容易。

## 2. 模块

```
main.go              组装：配置、托盘、窗口、命令、生命周期
config.go            config.json
shot.go              （-tags shot）真实窗口截图
internal/echonet/    帧编解码、UDP、发现、TID 匹配、重试（不懂 Sharp）
internal/sharp/      F1/F2/F3 解码、命令构造
internal/device/     单设备运行循环：轮询、通知、命令队列、回读确认
internal/homekit/    State ↔ HAP 映射、配对、重启
internal/history/    每分钟采样、月度 CSV、按天累加（用电、净化空气量）
internal/weather/    室外天气（Open-Meteo）、地点搜索、IP 估算位置
internal/firewall/   Windows 防火墙放行
internal/i18n/       中/日/英
internal/view/       原生 UI（只读 Model，通过回调发命令）
cmd/mokoctl/      只读 CLI
```

依赖方向：`view`、`homekit` → `device` → `sharp` → `echonet`。只有 `main` 和 `view` 依赖 MyGo，设备和 HomeKit 逻辑不依赖，也都有不需要真机的单元测试（用录下来的字节和模拟设备）。

## 3. 数据流

```
             ┌─ Get/Set ──▶ KI-SX100 (UDP 3610)
device 循环 ─┤◀─ INF/Res ──
   │  ▲
   │  └── 命令（串行）◀──┬── view（按钮）
   │                     └── homekit（Home 写入）
   └── State 快照 ──┬──▶ win.Update → view
                    ├──▶ characteristic.SetValue → Home
                    └──▶ history
```

要点：

- **只有 device 循环碰设备。** 所有请求串行，避免压垮设备。
- **命令是“写入 → 回读 → 发布”。** 回读确认后才算完成，然后读一次完整状态，再告诉调用方结果；失败时 UI 回到真实状态并提示。UI 里选的值先以 Pending 显示。
- **绘制绝不能发命令。** `ui.SegmentedBase` 会把 -1 的选中项改成 0，曾导致一渲染就要把设备切到静音，所以模式按钮用 `ButtonBase` 写，只响应点击，并有回归测试。
- **Home 写入要同步等结果。** 在 `SetValueRequestFunc` 里等回读，超时或失败返回 `JsonStatusServiceCommunicationFailure`。
- **MyGo 线程规则。** 视图回调在主线程，其他 goroutine 用 `win.Update(func(){…})` 改状态，主线程上不能阻塞等待。
- **hap 会丢弃相同值的写入**，所以每次轮询都要把真实状态同步给 HAP。

### 轮询

| 数据 | 频率 | 说明 |
|---|---|---|
| `80`、F3、F2、F1、`84`、`C0`、`88` | 30 秒；窗口或托盘面板可见时 10 秒 | 请求之间隔 100 ms |
| `85` | 5 分钟 | 累计电量 |
| `F0` | 连接后读一次 | 耗材上限，同一型号不变 |
| 收到主动通知 | 500 ms 后读一次 | 通知只当信号，不直接解析 |

单个请求超时 1.5 秒，重试 3 次；连续两轮失败转“离线”，保留最后读数，并按 5 秒到 60 秒退避重新发现。搜索顺序：手动 IP → 多播发现 → 上次的 IP。

### 数据存储

历史数据约每分钟一条，追加写到 `PathUserData/history/YYYY-MM.csv`，内存里保留 31 天，磁盘保留 13 个月。累计电量来自设备 `85`，按天累加增量算日用电；应用关闭期间的用电算在下次打开的那天。电价可设置，默认用 Sharp 规格页的 31 日元/kWh。

## 4. 应用形态

- 常驻托盘（macOS 菜单栏、Windows 通知区），`LSUIElement: true`，点击弹 flyout（参考 `examples/menubar-flyout`）。
- 主窗口按需打开，打开时切到 `ActivationPolicyRegular` 显示 Dock 图标；关窗只是隐藏，应用继续在托盘运行。
- 单实例（`App.RequestSingleInstanceLock`）：HAP 存储不能被两个实例同时使用。
- 开机自启：`App.SetOpenAtLogin`，设置页提供开关。

## 5. 发布与更新

`scripts/release.sh` 同时构建 macOS 和 Windows，签名、公证并上传到 GitHub 草稿 Release，发布草稿后已安装的应用就会更新。`UPLOAD=0` 只构建和校验，不上传。

mygo 先做一个普通 DMG 并公证 app；`scripts/make-dmg.sh` 再用 `assets/dmg/background.tiff` 重做带背景的 DMG，单独签名、公证、盖章。第一次运行时要允许终端控制 Finder。

- macOS：Developer ID 签名和公证，配置在 `mygo.json` 的 `macos.notarize`。
- Windows：安装包暂未做代码签名，SmartScreen 拦截时点“更多信息 → 仍要运行”。
- 自动更新：`plugins/updater/native`，公钥在 `mygo.json`，更新包必须用私钥签名才会被接受。**私钥不进仓库**，发布脚本从钥匙串读取（服务名 `moko-updater`）。
- Go 模块路径是 `github.com/qzz0518/moko`，`identifier` 发布后不要再变。

### 网络访问

只有设备和 Apple 家庭走局域网，其余三类走互联网：更新（GitHub）、天气（Open-Meteo）、位置估算（ipwho.is，失败时 ipapi.co，只在没有手动选地点时）。天气可以关闭。

## 6. 暂不做

- COCORO AIR 云
- Matter
- 多设备（`device` 层可以支持，UI 和 HAP 先按单台做）
- 无界面版本（模块已分离，Linux 或 NAS 常驻的需求出现后再加）
- 灰尘、气味的实时读数（没找到字段，见 device-echonet.md §4）
- 花粉、黄沙（COCORO AIR 的这两项来自 Weathernews，免费数据源覆盖不到日本）
