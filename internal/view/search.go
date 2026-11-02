package view

import (
	"runtime"

	"github.com/egoist/mygo/ui"

	"github.com/qzz0518/moko/internal/i18n"
)

// search is the overview until the device has been reached.
func (v *View) search(c *ui.Context, k pal) {
	m := v.M
	ui.Column(c).Grow(1).Center().Gap(18).Children(func() {
		switch {
		case m.Firewall == FirewallMissing || m.Firewall == FirewallBlocked:
			v.firewallCard(c.Key("firewall"), k)
			return
		case m.ListenErr != "":
			problem(c.Key("listen"), k, i18n.T("打不开网络端口", "ネットワークポートを開けません", "Cannot open the network port"), m.ListenErr)
		case m.Snap.Err == "":
			mokoTile(c.Key("moko"), 128, 0.55)
			ui.Column(c).Gap(5).AlignItems(ui.Center).Children(func() {
				ui.Text(c, i18n.T("正在寻找你的净化器", "空気清浄機を探しています", "Looking for your purifier")).FontSize(18).FontWeight(700)
				ui.Text(c, i18n.T("在局域网里搜索 KI-SX100", "ローカルネットワークで KI-SX100 を検索中", "Searching the local network for a KI-SX100")).FontSize(12.5).TextColor(k.muted)
			})
			return
		}
		mokoTile(c.Key("moko"), 88, -0.25)
		v.notFound(c.Key("notfound"), k)
	})
}

func problem(c *ui.Context, k pal, title, detail string) {
	ui.Column(c).Width(460).Gap(4).Children(func() {
		ui.Text(c, title).FontSize(14).FontWeight(600).TextColor(k.warn)
		ui.Text(c, detail).FontSize(11.5).TextColor(k.muted)
	})
}

func (v *View) notFound(c *ui.Context, k pal) {
	m := v.M
	card(c, k).Width(460).Padding(24).Gap(14).Children(func() {
		ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
			ui.Spinner(c).Label(i18n.T("搜索中", "検索中", "Searching"))
			ui.Text(c, i18n.T("还没有找到净化器", "空気清浄機が見つかりません", "No purifier found yet")).FontSize(16).FontWeight(700)
		})
		checks := []string{
			i18n.T("净化器已通电，并用 COCORO AIR App 连上了 Wi-Fi", "本体の電源が入り、COCORO AIR アプリで Wi-Fi に接続済み", "The purifier is plugged in and on Wi-Fi, set up with the COCORO AIR app"),
			i18n.T("这台电脑和净化器在同一个局域网", "このコンピュータと本体が同じネットワークにある", "This computer is on the same network as the purifier"),
		}
		switch runtime.GOOS {
		case "darwin":
			checks = append(checks, i18n.T("系统设置 › 隐私与安全性 › 本地网络 里允许了本应用", "システム設定 › プライバシーとセキュリティ › ローカルネットワーク で許可している", "System Settings › Privacy & Security › Local Network allows this app"))
		case "windows":
			checks = append(checks, i18n.T("Windows 把当前网络识别为“专用网络”", "Windows が現在のネットワークを「プライベート」と認識している", "Windows treats this network as Private"))
		}
		ui.Column(c).Gap(7).Children(func() {
			for i, s := range checks {
				ui.Row(c.Key(i)).Gap(8).Children(func() {
					ui.Text(c, "·").FontSize(13).TextColor(k.faint)
					ui.Text(c, s).FontSize(12.5).TextColor(k.muted).Grow(1).Basis(0)
				})
			}
		})
		if runtime.GOOS == "darwin" && m.OpenNetwork != nil {
			if ui.Button(c.Key("open"), i18n.T("打开本地网络设置", "ローカルネットワーク設定を開く", "Open Local Network Settings")).Clicked() {
				m.OpenNetwork()
			}
		}
		ui.Box(c).Height(1).Background(k.line)
		ui.Text(c, i18n.T("知道它的 IP 地址？可以直接连接：", "IP アドレスが分かる場合は直接接続できます：", "Know its IP address? Connect directly:")).FontSize(12.5).TextColor(k.muted)
		v.manualIPRow(c.Key("ip"), k, true)
	})
}

// manualIPRow edits the address to connect to. The search card always offers to try
// again; the settings page offers to apply only what has changed, since going back to
// searching would drop a working connection.
func (v *View) manualIPRow(c *ui.Context, k pal, retry bool) {
	m := v.M
	if !v.ipEdited {
		v.manualIP = m.ManualIP
	}
	ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
		in := ui.TextInput(c.Key("input"), &v.manualIP).Label(i18n.T("IP 地址", "IP アドレス", "IP address"))
		if retry {
			in.Grow(1).Placeholder(i18n.T("例如 192.168.1.20", "例：192.168.1.20", "e.g. 192.168.1.20"))
		} else {
			in.Width(controlWidth).Placeholder(i18n.T("自动搜索", "自動で検索", "Automatic"))
		}
		if in.Changed() {
			v.ipEdited = true
		}
		changed := v.manualIP != m.ManualIP
		apply := func() {
			v.ipEdited = false
			if m.SetManualIP != nil {
				m.SetManualIP(v.manualIP)
			}
		}
		if in.Submitted() && (changed || retry) {
			apply()
		}
		switch {
		case changed && v.manualIP != "":
			if ui.Button(c.Key("connect"), i18n.T("连接", "接続", "Connect")).Clicked() {
				apply()
			}
		case changed || retry:
			if ui.Button(c.Key("search"), i18n.T("重新搜索", "再検索", "Search again")).Clicked() {
				apply()
			}
		}
	})
}

// firewallCard asks first, instead of leaving it to Windows' own alert: canceling
// that one leaves rules behind that block the app from then on.
func (v *View) firewallCard(c *ui.Context, k pal) {
	m := v.M
	card(c, k).Width(460).Padding(24).Gap(12).Children(func() {
		ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
			ui.Text(c, i18n.T("需要允许局域网访问", "ローカルネットワークの許可が必要です", "Allow local network access")).FontSize(16).FontWeight(700)
		})
		ui.Text(c, i18n.T(
			"净化器通过局域网回复这台电脑，Windows 防火墙默认会挡住。放行需要确认一次管理员权限，只对专用网络生效。",
			"空気清浄機からの応答は Windows ファイアウォールに遮断されます。許可には管理者の確認が1回必要で、プライベートネットワークのみに適用されます。",
			"The purifier answers over the local network, and Windows Firewall blocks that by default. Allowing it takes one administrator prompt and applies to private networks only.",
		)).FontSize(12.5).TextColor(k.muted)
		ui.Row(c).Gap(8).Children(func() {
			if ui.PrimaryButton(c.Key("allow"), i18n.T("允许", "許可する", "Allow")).Clicked() && m.AllowFirewall != nil {
				m.AllowFirewall()
			}
			if ui.Button(c.Key("skip"), i18n.T("跳过", "スキップ", "Skip")).Clicked() && m.SkipFirewall != nil {
				m.SkipFirewall()
			}
		})
	})
}
