package view

import (
	"fmt"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/skip2/go-qrcode"

	"github.com/qzz0518/moko/internal/i18n"
)

// qrSVG draws text as a QR code, without its margin, and keeps the last one.
var qrCache struct {
	text string
	svg  *ui.SVG
}

func qrSVG(text string) *ui.SVG {
	if text == "" {
		return nil
	}
	if text == qrCache.text {
		return qrCache.svg
	}
	q, err := qrcode.New(text, qrcode.Medium)
	if err != nil {
		return nil
	}
	q.DisableBorder = true
	bm := q.Bitmap()
	var d strings.Builder
	for y, row := range bm {
		for x := 0; x < len(row); x++ {
			if row[x] {
				x0 := x
				for x < len(row) && row[x] {
					x++
				}
				fmt.Fprintf(&d, "M%d %dh%dv1h-%dz", x0, y, x-x0, x-x0)
			}
		}
	}
	svg := fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 %d %d"><path fill="#000" d="%s"/></svg>`, len(bm), len(bm), d.String())
	qrCache.text, qrCache.svg = text, ui.MustParseSVG([]byte(svg))
	return qrCache.svg
}

func (v *View) home(c *ui.Context, k pal) {
	m := v.M
	pageTitle(c, k, i18n.T("Apple 家庭", "Apple ホーム", "Apple Home"), i18n.T("在“家庭”App、Siri 和自动化里控制净化器", "「ホーム」アプリ、Siri、オートメーションで操作できます", "Control the purifier from the Home app, Siri and automations"))
	page := ui.Scroll(c.Key("scroll")).Grow(1).Gap(14)
	enabled := settled(page, m.HomeEnabled)
	paired := enabled && m.Home.Paired
	page.Children(func() {
		if paired {
			v.homePaired(c.Key("paired"), k)
		}
		card(c.Key("switch"), k).Children(func() {
			settingRow(c, k, i18n.T("加入 Apple 家庭", "Apple ホームに追加", "Add to Apple Home"), i18n.T("一个家只需要在一台电脑上打开", "1つの家につき1台のコンピュータでオンにしてください", "Turn this on for one computer in a home"), func() {
				want := m.HomeEnabled
				if ui.Switch(c.Key("on"), &want).Label(i18n.T("加入 Apple 家庭", "Apple ホームに追加", "Add to Apple Home")).Changed() && m.SetHomeEnabled != nil {
					m.SetHomeEnabled(want)
				}
			})
			if enabled {
				hairline(c, k)
				settingRow(c, k, i18n.T("防止电脑自动睡眠", "コンピュータのスリープを防ぐ", "Keep this computer awake"), i18n.T("电脑睡眠或应用退出时，“家庭”里会显示“无响应”", "スリープ中やアプリ終了中は「ホーム」に「応答なし」と表示されます", "While it sleeps or the app is closed, Home shows No Response"), func() {
					want := m.KeepAwake
					if ui.Switch(c.Key("awake"), &want).Label(i18n.T("防止电脑自动睡眠", "コンピュータのスリープを防ぐ", "Keep this computer awake")).Changed() && m.SetKeepAwake != nil {
						m.SetKeepAwake(want)
					}
				})
			}
			if paired {
				hairline(c, k)
				settingRow(c, k, i18n.T("解除配对", "ペアリングを解除", "Remove from Home"), i18n.T("从所有家庭移除，并生成新的配对码", "すべてのホームから削除し、新しいコードを作ります", "Removes it from every Home and makes a new code"), func() {
					smallButton(c.Key("reset"), k, i18n.T("解除", "解除", "Remove"), nil, func() { v.askReset = true })
				})
			}
		})
		if m.Home.Err != "" {
			banner(c.Key("err"), k, k.bad, icAlert, m.Home.Err)
		}
		if m.Firewall == FirewallMissing || m.Firewall == FirewallBlocked {
			banner(c.Key("fw"), k, k.warn, icShield, i18n.T("Windows 防火墙还没有放行，iPhone 连不上这台电脑", "Windows ファイアウォールが未許可のため、iPhone から接続できません", "Windows Firewall has not allowed this app, so an iPhone cannot reach this computer"))
			if ui.Button(c.Key("allow"), i18n.T("允许访问", "許可する", "Allow")).Clicked() && m.AllowFirewall != nil {
				m.AllowFirewall()
			}
		}
		switch {
		case paired:
		case enabled && m.Home.SetupURI != "":
			v.homePairing(c.Key("pairing"), k)
		case enabled:
			ui.Row(c.Key("starting")).Gap(8).Padding(16).Children(func() {
				ui.Spinner(c).Label(i18n.T("启动中", "起動中", "Starting"))
				ui.Text(c, i18n.T("正在启动", "起動中", "Starting")).TextColor(k.muted)
			})
		default:
			v.homeIntro(c.Key("intro"), k)
		}
	})
	switch ui.AlertDialog(c, &v.askReset,
		i18n.T("解除配对？", "ペアリングを解除しますか？", "Remove from Home?"),
		i18n.T("净化器会从“家庭”里消失，相关的场景和自动化也要重新设置。", "「ホーム」から消え、関連するシーンとオートメーションは再設定が必要です。", "It disappears from Home, and scenes and automations that use it need setting up again."),
		i18n.T("取消", "キャンセル", "Cancel"), i18n.T("解除", "解除", "Remove")) {
	case 1:
		if m.ResetHome != nil {
			m.ResetHome()
		}
	}
}

func (v *View) homeIntro(c *ui.Context, k pal) {
	card(c, k).Padding(20).Gap(10).Children(func() {
		ui.Text(c, i18n.T("打开后可以在“家庭”里看到", "オンにすると「ホーム」に表示されるもの", "With this on, Home shows")).FontSize(14).FontWeight(700)
		for i, s := range []string{
			i18n.T("空气净化：开关、自动或手动、三档风量", "空気清浄：オン/オフ、自動/手動、風量3段階", "Air purifier: on or off, auto or manual, three fan speeds"),
			i18n.T("加湿：开关、当前湿度、水箱状态", "加湿：オン/オフ、現在の湿度、水タンク", "Humidifier: on or off, humidity now, water level"),
			i18n.T("空气质量、PM2.5、室温和滤网寿命，可用于自动化", "空気質、PM2.5、室温、フィルター寿命（オートメーションに利用可）", "Air quality, PM2.5, temperature and filter life, for automations"),
			i18n.T("夜间和花粉模式的开关", "おやすみ・花粉モードのスイッチ", "Switches for night and pollen modes"),
		} {
			ui.Row(c.Key(i)).Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Text(c, "·").FontSize(13).TextColor(k.faint)
				ui.Text(c, s).FontSize(12.5).TextColor(k.muted)
			})
		}
	})
}

func (v *View) homePairing(c *ui.Context, k pal) {
	m := v.M
	card(c, k).Padding(22).Children(func() {
		ui.Row(c).Gap(26).AlignItems(ui.Center).Children(func() {
			ui.Box(c).Size(176, 176).Padding(14).Radius(14).Border(1, k.line).Background(ui.RGB(255, 255, 255)).Center().TextColor(ui.RGB(0, 0, 0)).Role(ui.RoleImage).Label(i18n.T("配对二维码", "ペアリング QR コード", "Pairing QR code")).Children(func() {
				if svg := qrSVG(m.Home.SetupURI); svg != nil {
					ui.Icon(c, svg).Size(148, 148)
				}
			})
			ui.Column(c).Grow(1).Basis(0).Gap(12).Children(func() {
				ui.Column(c).Gap(2).Children(func() {
					ui.Text(c, i18n.T("配对码", "セットアップコード", "Setup code")).FontSize(12).TextColor(k.muted)
					ui.Text(c, m.Home.Code).FontSize(28).FontWeight(800).FontFeatures("tnum").LetterSpacing(1).Selectable()
				})
				for i, s := range []string{
					i18n.T("在 iPhone 的“家庭”App 里点“添加配件”", "iPhone の「ホーム」で「アクセサリを追加」をタップ", "In the Home app on iPhone, tap Add Accessory"),
					i18n.T("扫描二维码；扫不了就选“更多选项”，输入配对码", "QR コードをスキャン。できない場合は「その他のオプション」でコードを入力", "Scan the code, or choose More Options and type the setup code"),
					i18n.T("提示“未认证配件”时选“仍然添加”", "「認定されていないアクセサリ」と表示されたら「このまま追加」", "If it warns of an uncertified accessory, choose Add Anyway"),
				} {
					ui.Row(c.Key(i)).Gap(8).Children(func() {
						ui.Text(c, fmt.Sprint(i+1)).FontSize(12).FontWeight(800).TextColor(k.muted).FontFeatures("tnum")
						ui.Text(c, s).FontSize(12.5).TextColor(k.muted).Grow(1).Basis(0)
					})
				}
			})
		})
	})
}

// homePaired says it is in Home, and what is there, in front of a house of its own: it
// is the good news of this page.
func (v *View) homePaired(c *ui.Context, k pal) {
	a := air{sky: homeSky, mood: calmMood(1)}
	a.mood.shade, a.mood.deep = a.sky.shade, a.sky.deep
	a.mood.breath = 3600 * time.Millisecond
	box, _ := skyBox(c, a, 20)
	homeDecor(c, box, a.sky)
	box.Height(190).Children(func() {
		mascot(c.Key("moko"), 196, a.mood, "Moko", "").Absolute().Right(-10).Bottom(-10)
		ui.Column(c.Key("words")).Fill().Padding(22, 24).Children(func() {
			ui.Row(c.Key("title")).Gap(7).AlignItems(ui.Center).Children(func() {
				ui.Icon(c, icCheck).FontSize(19)
				ui.Text(c, i18n.T("已加入家庭", "ホームに追加済み", "In Apple Home")).FontSize(19).FontWeight(800)
			})
			ui.Text(c, i18n.T("“家庭”App 和 Siri 现在可以控制这些：", "「ホーム」アプリと Siri で使えるもの：", "The Home app and Siri can now reach:")).FontSize(12.5).FontWeight(600).Margin(5, 0, 0, 0)
			ui.Spacer(c)
			ui.Column(c.Key("parts")).Gap(6).Children(func() {
				for y, row := range [][]string{
					{
						i18n.T("空气净化", "空気清浄", "Air purifier"),
						i18n.T("加湿", "加湿", "Humidifier"),
						i18n.T("空气质量", "空気質", "Air quality"),
						i18n.T("室温", "室温", "Temperature"),
					},
					{
						i18n.T("滤网寿命", "フィルター寿命", "Filter life"),
						i18n.T("夜间模式", "おやすみ", "Night mode"),
						i18n.T("花粉模式", "花粉", "Pollen mode"),
					},
				} {
					ui.Row(c.Key(y)).Gap(6).Children(func() {
						for x, s := range row {
							ui.Box(c.Key(x)).Padding(4, 9).Radius(8).Background(ui.RGBA(255, 255, 255, 0.4)).Children(func() {
								ui.Text(c, s).FontSize(12).FontWeight(600).NoWrap()
							})
						}
					})
				}
			})
		})
	})
}
