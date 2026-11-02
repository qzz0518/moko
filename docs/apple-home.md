# 接入 Apple 家庭

**结论：用 HAP（`brutella/hap`），进程内纯 Go，不走 Charker 的 Matter 方案。** 直接参考 Moniflip：它已经用同样的技术栈（MyGo 原生 UI + `brutella/hap`）发布了 macOS 和 Windows 版。

实现在 `internal/homekit`。**还没有和 iPhone 配对过**，§3 末尾的问题要配对后才知道。

## 1. 为什么和 Charker 不同

决定因素是**加湿**。这台设备一半的功能是加湿，而 Matter 路线在 Apple 家庭里没有加湿器。

iOS 26 自带的 [`chipplugin-protocol-map`](https://github.com/RiDDiX/home-assistant-matter-hub/discussions/410)（社区导出，不是 Apple 公开文档）描述了 Matter 到 HAP 的映射：

- 设备类型 `45`（Air Purifier）映射到 HAP 服务 `BB`，可关联 `8D`（空气质量）和 `BA`（滤网）
- 全文没有 `BD`（HumidifierDehumidifier），也没有任何加湿器设备类型

| | HAP（选用） | Matter（Charker 路线） |
|---|---|---|
| 净化器 | ✅ `BB` | ✅ `0x002D` |
| 风量、自动/手动 | ✅ | ✅ |
| **加湿器** | ✅ `BD`，含水位 | ❌ 只能退化成一个开关 |
| 空气质量、PM2.5 | ✅ | ✅ |
| 滤网 | ✅ | ✅ |
| 实现 | Go 库，进程内 | Rust helper（`rs-matter` fork），子进程 |
| 工具链 | 只要 Go，可交叉编译 | 额外要 Rust，Windows 要 MSVC 或 mingw |
| 其他生态 | 仅 Apple | Google、Alexa 等也能接 |
| 库的活跃度 | 最后提交 2024-10，但已有 Moniflip 在用 | 依赖自己维护的 fork |

Charker 选 Matter 有它的理由：要显示端口功率（需 iOS 27），这是 HAP 给不了的。这里的需求正好相反。

**以后想接 Google Home 或 Alexa 时**，再考虑加 Matter。Charker 的 helper 结构可以复用。

## 2. 可以直接借鉴的实现

### Moniflip（[homekit.go](https://github.com/qzz0518/Moniflip/blob/main/homekit.go)）

| 做法 | 为什么 |
|---|---|
| `setupCodes` 随机生成 PIN 和 Setup ID，存到 `hap.Store` | 不用默认 PIN `00102003` |
| `setupURI` 拼 `X-HM://`，`go-qrcode` 画二维码 | 扫码配对 |
| `prev`/`done`：等上个服务完全停下再启动 | 上个服务的 Bonjour 告别晚到，会把新公告从 iPhone 缓存里删掉 |
| `unpairHomeKit` 清空整个存储 | 否则 Home 里离线删除后，本机仍认为已配对，不再接受新配对 |
| 固定 `acc.Id` 和服务、特征的 IID | 自动化按 IID 记忆 |
| 同时设 `Name` 和 `ConfiguredName` | iOS 16 起 Home 用后者给服务命名 |
| `SetValueRequestFunc` 先执行再接受，失败返回 `JsonStatusServiceCommunicationFailure` | Home 能显示失败，不会假装成功 |
| 定时同步实际状态（30 秒） | hap 会丢弃与当前值相同的写入；状态不同步时按钮就按不动了 |
| `mygo.RunOnMain` / `win.Update` 回主线程 | hap 回调不在主线程 |
| `hlog.Info.Disable()` | 关闭日志噪音 |
| `mygo.json` 的 `macos.infoPlist` | 见 §5 |

### Charker（[firewall_windows.go](https://github.com/qzz0518/Charker/blob/main/charker-go/internal/app/firewall_windows.go)）

- 读规则不需要提权；需要时弹一次 UAC，删掉旧规则再加放行规则。
- 动机：Windows 防火墙弹窗上点“取消”会留下**入站阻止规则**，之后 Home 能发现设备却连不上。
- Moniflip 目前只依赖系统弹窗，这里采用 Charker 的做法更稳。
- Charker 的规则是 `-Protocol UDP`，**这里不够用**：HAP 走 TCP，所以 `internal/firewall` 按程序路径放行，不限协议。

配对页的状态设计（未配对、已配对、mDNS 失败、防火墙拦截）也可以照搬 Charker 的 `HomeState`。

## 3. 配件设计

一个独立配件，不用 Bridge，类别 `TypeAirPurifier`。只有一台设备，用不上 Bridge；hap 也有 Bridge 状态不同步的 issue（[#58](https://github.com/brutella/hap/issues/58)）。

配件名用 ASCII（`Moko`，型号字段是 `KI-SX100`）：hap 有中文名连接问题的 issue（[#54](https://github.com/brutella/hap/issues/54)）。中文显示名放到 `ConfiguredName`。

| HAP 服务 | 特征 | 设备字段 | 说明 |
|---|---|---|---|
| **AirPurifier** `BB`（主服务） | Active | `80` | |
| | CurrentAirPurifierState | `80` | 关→0，开→2 |
| | TargetAirPurifierState | F3[4] | 自动→1，其余→0 |
| | RotationSpeed（需手动 `AddC`） | F3[4] | 33/66/100 ↔ 静音/中/强，步长 33 |
| **HumidifierDehumidifier** `BD` | Active | F3[15] | |
| | CurrentHumidifierDehumidifierState | F2[24] | 关→0，待机→1，加湿中→2 |
| | TargetHumidifierDehumidifierState | 固定 | `ValidVals` 限定为“加湿” |
| | CurrentRelativeHumidity | F1[4] | |
| | WaterLevel（需手动 `AddC`） | F2[19] | 有水 100，缺水 0 |
| **AirQualitySensor** `8D` | AirQuality | F1[27:28] | 按下表换算 |
| | PM2_5Density（需手动 `AddC`） | F1[27:28] | |
| **TemperatureSensor** `8A` | CurrentTemperature | F1[3] | |
| **Switch** ×2 | On | F3[4] | 夜间、花粉；Home 没有“模式”的概念，只能用开关。关掉后回到自动 |

`FilterMaintenance`（`BA`）关联在净化服务上。寿命取集尘滤网的剩余（F0/F1，和 COCORO AIR 对照过），剩 10% 及以下时提示更换，这个阈值是我们定的。F2[23] 含义未确认，不用它。

设备字段的含义和可信度见 [device-echonet.md](device-echonet.md)。

补充行为：

- 设备关机时，模式和加湿写入会先开机；关加湿不会开机。
- 风速滑块吸附到最近一档（33/66/100）；在自动模式下显示最近一次的手动档位。
- Home 写入会等设备回读确认，最长 12 秒；失败时 Home 显示失败。
- 服务和特征的 IID 用固定基数（100、200…）。**以后新增服务要用新基数，新增特征放末尾**，否则用户的自动化会失效。

**PM2.5 → AirQuality**（初版，可调）：

| µg/m³ | ≤12 | ≤35 | ≤55 | ≤150 | >150 | 无效 |
|---|---|---|---|---|---|---|
| 值 | 1 优 | 2 良 | 3 中 | 4 差 | 5 很差 | 0 未知 |

### 需要在 Home App 里实测的点

这些无法从代码推断，要配对后看：

1. **目标湿度。** 协议里没找到目标湿度字段，所以目前用 `NewHumidifierDehumidifier()`，不提供阈值，避免 Home 出现一个不起作用的滑块。Homebridge 插件则是把阈值固定为 55%。如果 Home 在没有阈值时显示不正常，再照它的做法改。
2. **一个配件多个服务的呈现。** Home 默认可能合并成一张拼贴。净化器和加湿器的控制是否都容易找到？
3. **自动 ↔ 手动切换的表现。** 用户在 Home 拖风速时，要切到手动档位。

## 4. 可用性

- **运行应用的电脑要保持唤醒。** 电脑睡眠或应用退出后，Home 显示“无响应”。Charker 和 Moniflip 都是这样。
- **“防止电脑自动睡眠”选项**：`mygo.Power.KeepAwake`。默认关闭，只防止空闲睡眠，只在开启 Apple 家庭时生效。
- **唤醒和换网后要重启 HAP 服务。** `brutella/dnssd` 只在 Linux 上监听网络变化（spike 日志：`unable to wait for link updates`）。应用在 `mygo.Power.OnResume` 和 `mygo.Network.OnChanged` 时重启服务（3 秒防抖），走上面的 `prev`/`done` 流程，并重新加入 ECHONET 多播组。
- **开机自启**：`mygo.App.SetOpenAtLogin`，配合 `WasOpenedAtLogin` 启动时不显示窗口。
- **远程控制和自动化需要家庭中枢**（HomePod 或 Apple TV），应用这边不用做什么。
- **一个家只开一台电脑。** 多台电脑都打开开关，Home 里会出现多个配件。开关默认关闭，页面上说明这一点。

## 5. 平台细节

### macOS

依据 Apple [TN3179](https://developer.apple.com/documentation/technotes/tn3179-understanding-local-network-privacy)：

- 多播、mDNS、局域网单播都受“本地网络”权限管控。**multicast entitlement 在 macOS 上不需要。**
- **从终端运行的程序免检。** `go run` 不会触发弹窗，这个流程只能用打包好的 `.app` 测试。
- 首次访问可能在用户回答前就被拒绝，代码要重试。ECHONET 发现和 HAP 广播都要处理。
- 权限被拒后要明确提示，并给出去系统设置开启的路径。

```json
"macos": {
  "minimumSystemVersion": "13.0",
  "infoPlist": {
    "LSUIElement": true,
    "NSLocalNetworkUsageDescription": "Moko uses the local network to control your air purifier and to connect it to Apple Home.",
    "NSBonjourServices": ["_hap._tcp"]
  }
}
```

和系统 mDNSResponder 共用 5353 没有问题（spike 已验证，`dns-sd` 能看到实例）。

### Windows

入站放行范围比 Charker 多，按程序路径放行，协议不限：

| 流量 | 协议 | 原因 |
|---|---|---|
| HAP | TCP（随机端口，可用 `server.Addr` 固定） | iPhone 主动连入 |
| mDNS | UDP 5353 | 配件发现 |
| ECHONET | UDP 3610 | 设备主动推送的 INF 是入站流量；请求的回复也只发到 3610 |

其他：

- 网络被识别成“公用”时规则不生效。Moniflip 的 README 里已经提示用户，这里同样要提示。
- 应用启动时先检查规则：没放行就先显示说明卡片，点“允许”弹一次 UAC；也可以跳过，交给系统弹窗。
- 多网卡（Hyper-V、WSL、VPN）目前全部广播，和 Moniflip 一样。出问题时可以用 `server.Ifaces` 限定。
- Windows 上未签名的安装包会被 SmartScreen 拦截。Moniflip 目前未签名，见 [architecture.md](architecture.md)。

## 6. 配对流程

1. 设置 → Apple 家庭 → 打开开关，显示二维码和 8 位配对码。
2. iPhone 家庭 App → 添加配件 → 扫码。
3. 提示“未认证配件”时选“仍然添加”。
4. 配件数据存在 `PathUserData/homekit`（`hap.NewFsStore`）。

关闭开关只是让配件下线，配对还在，再打开会恢复。要彻底移除，点“解除配对”：清空这个目录，并生成新的配对码。这一点和 Moniflip 不同，它关开关就解除配对。

## 7. 许可与合规

`brutella/hap` 是 Apache-2.0。它基于 Apple 公开的**非商用版** HAP 规范。开源或自用没有问题（Homebridge、Moniflip 都是这样），不过配件会显示“未认证”。如果以后要商业销售，要先查 Apple MFi 条款，这次调研没有核实具体要求。
