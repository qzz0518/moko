package view

import (
	"fmt"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/qzz0518/moko/internal/device"
	"github.com/qzz0518/moko/internal/i18n"
)

var languageCodes = []string{"", "zh", "ja", "en"}

// The controls of the settings line up on their right edge, at one width.
const controlWidth = 150

func (v *View) settings(c *ui.Context, k pal) {
	pageTitle(c, k, i18n.T("设置", "設定", "Settings"), "")
	ui.Scroll(c.Key("scroll")).Grow(1).Children(func() {
		// As tall as the column on the right, whichever is taller.
		ui.Row(c.Key("cols")).Gap(12).AlignItems(ui.Stretch).Children(func() {
			v.deviceCard(c.Key("device"), k)
			ui.Column(c.Key("right")).Grow(1).Basis(0).MinWidth(0).Gap(12).Children(func() {
				v.preferences(c.Key("prefs"), k)
				v.about(c.Key("about"), k)
			})
		})
	})
}

// deviceCard is the device: its sky on top, as on the overview, and below it what it
// is and how its consumables are doing.
func (v *View) deviceCard(c *ui.Context, k pal) {
	m := v.M
	card(c, k).Width(296).Clip().Children(func() {
		// This card is about the connection: its color and signal say whether the device
		// answers, whatever the air is like.
		state := i18n.T("搜索中", "検索中", "Searching")
		a := air{sky: unknownSky, mood: calmMood(0.1)}
		switch m.Snap.Phase {
		case device.PhaseOnline:
			state = i18n.T("已连接", "接続中", "Connected")
			a = air{sky: deviceSky, mood: calmMood(0.55)}
		case device.PhaseOffline:
			state = i18n.T("已断开，正在重连", "切断、再接続中", "Lost, reconnecting")
			a.mood = calmMood(-0.45)
		}
		a.mood.shade, a.mood.deep = a.sky.shade, a.sky.deep
		a.mood.breath = 3600 * time.Millisecond
		box, fg := skyBox(c.Key("sky"), a, 0)
		signalDecor(c, box, fg, m.Snap.Phase != device.PhaseOffline)
		box.Height(124).Children(func() {
			mascot(c.Key("moko"), 126, a.mood, "Moko", state).Absolute().Right(-6).Bottom(-6)
			ui.Column(c.Key("words")).Fill().Padding(14, 16).Children(func() {
				ui.Text(c, "KI-SX100").FontSize(17).FontWeight(700)
				ui.Spacer(c)
				ui.Text(c, state).FontSize(12).FontWeight(600)
			})
		})
		ui.Column(c.Key("body")).Grow(1).Padding(16).Gap(14).Children(func() {
			ui.Column(c.Key("info")).Gap(7).Children(func() {
				info := m.Snap.Info
				infoLine(c.Key("module"), k, i18n.T("无线模块", "無線モジュール", "Wireless module"), orDash(info.Module))
				infoLine(c.Key("firmware"), k, i18n.T("固件", "ファームウェア", "Firmware"), orDash(info.Firmware))
				infoLine(c.Key("ip"), k, i18n.T("IP 地址", "IP アドレス", "IP address"), orDash(info.IP))
				cleaned := "—"
				if st := m.Snap.State; st.SensorsOK {
					cleaned = thousands(int64(st.Sensors.AirCleaned)) + " m³"
				}
				infoLine(c.Key("air"), k, i18n.T("累计净化空气", "きれいにした空気量", "Air cleaned"), cleaned)
			})
			ui.Box(c.Key("line")).Height(1).Background(k.line)
			v.supplies(c.Key("supplies"), k)
		})
	})
}

func infoLine(c *ui.Context, k pal, label, value string) {
	ui.Row(c).AlignItems(ui.Center).Gap(8).Children(func() {
		ui.Text(c, label).FontSize(12).TextColor(k.muted).Grow(1).NoWrap()
		ui.Text(c, value).FontSize(12).FontWeight(600).FontFeatures("tnum").NoWrap().Selectable()
	})
}

// supplies says how much of each consumable is left.
func (v *View) supplies(c *ui.Context, k pal) {
	st := v.M.Snap.State
	ui.Column(c).Gap(10).Children(func() {
		if !st.LimitsOK || !st.SensorsOK {
			ui.Text(c, i18n.T("还没有读到耗材状态", "消耗品の状態を読み取っています", "Waiting for the consumables")).FontSize(12).TextColor(k.faint)
			return
		}
		left := st.Limits.Left(st.Sensors.Used)
		for i, s := range []struct {
			name string
			pct  int
		}{
			{i18n.T("集尘滤网", "集じんフィルター", "Dust filter"), left.Dust},
			{i18n.T("脱臭滤网", "脱臭フィルター", "Deodorizing filter"), left.Odor},
			{i18n.T("加湿滤网", "加湿フィルター", "Humidifying filter"), left.Humidifying},
			{i18n.T("Ag⁺ 离子盒", "Ag+イオンカートリッジ", "Ag+ cartridge"), left.Silver},
			{i18n.T("离子发生单元", "イオン発生ユニット", "Ion unit"), left.Ion},
		} {
			supplyRow(c.Key(i), k, s.name, s.pct)
		}
		if st.NeedsCleaning {
			ui.Text(c.Key("clean"), i18n.T("设备提示该清洁了", "お手入れの時期です", "The device asks to be cleaned")).FontSize(12).FontWeight(600).TextColor(k.warn)
		}
	})
}

func supplyRow(c *ui.Context, k pal, name string, pct int) {
	col := k.accent
	switch {
	case pct <= 10:
		col = k.bad
	case pct <= 30:
		col = k.warn
	}
	ui.Column(c).Gap(4).Children(func() {
		ui.Row(c).AlignItems(ui.Center).Children(func() {
			ui.Text(c, name).FontSize(12).Grow(1).NoWrap()
			ui.Text(c, fmt.Sprintf("%d%%", pct)).FontSize(12).FontWeight(700).FontFeatures("tnum").NoWrap()
		})
		ui.Box(c).Height(4).Radius(2).Background(k.card2).Draw(func(p *ui.Painter, r ui.Rect) {
			p.Fill(ui.Rect{X: r.X, Y: r.Y, W: max(r.W*float32(pct)/100, 4), H: r.H}, col, 2)
		})
	})
}

// preferences is what the user decides.
func (v *View) preferences(c *ui.Context, k pal) {
	m := v.M
	card(c, k).Children(func() {
		settingRow(c, k, i18n.T("开机时启动", "ログイン時に起動", "Open at login"), i18n.T("在后台运行，保持 Apple 家庭可用", "バックグラウンドで動作し、Apple ホームを利用可能にします", "Runs in the background and keeps Apple Home available"), func() {
			want := m.OpenAtLogin
			if ui.Switch(c.Key("login"), &want).Label(i18n.T("开机时启动", "ログイン時に起動", "Open at login")).Changed() && m.SetOpenAtLogin != nil {
				m.SetOpenAtLogin(want)
			}
		})
		hairline(c, k)
		settingRow(c, k, i18n.T("外观", "外観", "Appearance"), "", func() {
			codes := []string{"", "light", "dark"}
			opts := []choice{
				{label: i18n.T("自动", "自動", "Auto"), on: m.Appearance == ""},
				{label: i18n.T("浅色", "ライト", "Light"), on: m.Appearance == "light"},
				{label: i18n.T("深色", "ダーク", "Dark"), on: m.Appearance == "dark"},
			}
			ui.Box(c.Key("appearance")).Width(controlWidth).Children(func() {
				chooser(c, k, "track", i18n.T("外观", "外観", "Appearance"), false, opts, false, func(i int) {
					if m.SetAppearance != nil {
						m.SetAppearance(codes[i])
					}
				})
			})
		})
		hairline(c, k)
		settingRow(c, k, i18n.T("缺水时通知我", "水がなくなったら通知", "Notify me when out of water"), "", func() {
			want := m.WaterAlert
			if ui.Switch(c.Key("water"), &want).Label(i18n.T("缺水时通知我", "水がなくなったら通知", "Notify me when out of water")).Changed() && m.SetWaterAlert != nil {
				m.SetWaterAlert(want)
			}
		})
		hairline(c, k)
		settingRow(c, k, i18n.T("语言", "言語", "Language"), "", func() {
			names := []string{i18n.T("跟随系统", "システムに従う", "System"), "简体中文", "日本語", "English"}
			cur := names[0]
			for i, code := range languageCodes {
				if code == m.Language {
					cur = names[i]
				}
			}
			if ui.Select(c.Key("lang"), &cur, names).Width(controlWidth).Label(i18n.T("语言", "言語", "Language")).Changed() && m.SetLanguage != nil {
				for i, n := range names {
					if n == cur {
						m.SetLanguage(languageCodes[i])
					}
				}
			}
		})
		hairline(c, k)
		settingRow(c, k, i18n.T("电价", "電気料金", "Electricity price"), i18n.T("日元/kWh", "円/kWh", "Yen per kWh"), func() {
			stepper(c.Key("price"), k, i18n.T("电价", "電気料金", "Electricity price"), m.Price, 1, 200, func(x float64) {
				if m.SetPrice != nil {
					m.SetPrice(x)
				}
			})
		})
		hairline(c, k)
		settingRow(c, k, i18n.T("手动指定 IP", "IP を手動で指定", "Fixed IP address"), i18n.T("搜索不到设备时才需要", "見つからない場合のみ", "Only if the search finds nothing"), func() {
			v.manualIPRow(c.Key("ip"), k, false)
		})
	})
}

// stepper changes a number by ones, in the look of the modes' track.
func stepper(c *ui.Context, k pal, label string, value, lo, hi float64, set func(float64)) {
	ui.Row(c).Width(controlWidth).Height(34).Padding(2).Radius(10).Background(k.card2).AlignItems(ui.Center).Role(ui.RoleGroup).Label(label).Children(func() {
		step := func(key string, ic *ui.SVG, name string, delta float64) {
			b := ui.ButtonBase(c.Key(key)).Size(30, 30).Radius(8).Center().Label(name).Disabled(value+delta < lo || value+delta > hi)
			if b.Clicked() && value+delta >= lo && value+delta <= hi {
				set(value + delta)
			}
			bg := ui.Transparent
			if b.Hovered() {
				bg = k.hover
			}
			press(b).Background(b.AnimateColor("bg", bg, ui.Animation{Duration: 120 * time.Millisecond})).Children(func() {
				ui.Icon(c, ic).FontSize(14).TextColor(k.muted)
			})
		}
		step("down", icMinus, i18n.T("减少", "減らす", "Less"), -1)
		ui.Text(c.Key("value"), fmt.Sprintf("%.0f", value)).Grow(1).TextAlign(ui.Center).FontSize(13).FontWeight(700).FontFeatures("tnum")
		step("up", icPlus, i18n.T("增加", "増やす", "More"), 1)
	})
}

// Where the project lives.
const (
	authorX  = "https://x.com/zerah_eth"
	repoLink = "https://github.com/qzz0518/moko"
)

// about says what this is and who made it, and has the way out, which the app has no
// other place for: the close button of its window only hides it.
func (v *View) about(c *ui.Context, k pal) {
	m := v.M
	card(c, k).Children(func() {
		ui.Row(c.Key("app")).MinHeight(60).Padding(10, 16).Gap(11).AlignItems(ui.Center).Children(func() {
			brandMark(c, 36)
			ui.Column(c).Grow(1).Basis(0).MinWidth(0).Gap(1).Children(func() {
				ui.Text(c, "Moko").FontSize(14).FontWeight(800)
				ui.Text(c, i18n.T("版本 ", "バージョン ", "Version ")+orDash(m.Version)).FontSize(11.5).FontFeatures("tnum").TextColor(k.muted)
			})
			smallButton(c.Key("update"), k, i18n.T("检查更新", "アップデートを確認", "Check for Updates"), nil, func() {
				if m.CheckUpdate != nil {
					m.CheckUpdate()
				}
			})
		})
		hairline(c, k)
		settingRow(c.Key("author"), k, i18n.T("作者与源码", "作者とソース", "Author and source"), "", func() {
			open := func(url string) func() {
				return func() {
					if m.OpenURL != nil {
						m.OpenURL(url)
					}
				}
			}
			ui.Row(c.Key("links")).Gap(6).Children(func() {
				smallButton(c.Key("x"), k, "X @zerah_eth", icOpen, open(authorX)).Tooltip(authorX)
				smallButton(c.Key("github"), k, "GitHub", icOpen, open(repoLink)).Tooltip(repoLink)
			})
		})
		hairline(c, k)
		note := ""
		if m.HomeEnabled {
			note = i18n.T("退出后“家庭”里会显示“无响应”", "終了すると「ホーム」に「応答なし」と表示されます", "Home shows No Response after quitting")
		}
		settingRow(c.Key("quit"), k, i18n.T("退出 Moko", "Moko を終了", "Quit Moko"), note, func() {
			smallButton(c.Key("quit"), k, i18n.T("退出", "終了", "Quit"), nil, func() {
				if m.Quit != nil {
					m.Quit()
				}
			})
		})
	})
}

// smallButton is the button at the end of a row, in the look of the stepper.
func smallButton(c *ui.Context, k pal, text string, ic *ui.SVG, click func()) ui.Element {
	b := ui.ButtonBase(c).Height(30).Padding(0, 11).Gap(5).Radius(9).AlignItems(ui.Center).Label(text)
	if b.Clicked() && click != nil {
		click()
	}
	bg := k.card2
	if b.Hovered() {
		bg = k.card2.Mix(k.text, 0.07)
	}
	return press(b).Background(b.AnimateColor("bg", bg, ui.Animation{Duration: 120 * time.Millisecond})).Children(func() {
		ui.Text(c, text).FontSize(12).FontWeight(600).NoWrap()
		if ic != nil {
			ui.Icon(c, ic).FontSize(11).TextColor(k.muted)
		}
	})
}

func orDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}
