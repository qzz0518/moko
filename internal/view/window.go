package view

import (
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/qzz0518/moko/internal/device"
	"github.com/qzz0518/moko/internal/i18n"
)

// Main is the view of the main window.
func (v *View) Main(c *ui.Context) {
	k := v.setup(c)
	ui.Row(c).Fill().AlignItems(ui.Stretch).Background(k.bg).TextColor(k.text).FontSize(13).Lang(langTag()).Children(func() {
		v.sidebar(c.Key("side"), k)
		ui.Box(c.Key("divider")).Width(1).Background(k.line)
		bar := c.TitleBar()
		top := max(bar.Height, 22)
		ui.Box(c.Key("content")).Grow(1).Basis(0).Children(func() {
			// A page coming in rises a little as it fades in; the one it replaces just goes.
			ui.Column(c.Key(v.Page)).Fill().Padding(top, 18, 18, 18).Gap(12).Transition(ui.ElementTransition{
				Duration: 240 * time.Millisecond, Ease: ui.CubicBezier(0.32, 0.72, 0, 1), Enter: &ui.Motion{Y: 8},
			}).Children(func() {
				switch {
				case v.Page == PageOverview && !v.M.Snap.HasState:
					v.search(c, k)
				case v.Page == PageOverview:
					v.overview(c, k)
				case v.Page == PageWeather:
					v.weatherPage(c, k)
				case v.Page == PageTrends:
					v.trends(c, k)
				case v.Page == PageEnergy:
					v.energy(c, k)
				case v.Page == PageHome:
					v.home(c, k)
				case v.Page == PageSettings:
					v.settings(c, k)
				}
			})
			// The strip the window is dragged by, clear of the window's buttons.
			ui.Box(c.Key("drag")).Absolute().Top(0).Left(0).Right(bar.Right).Height(top).DragWindow()
		})
	})
}

const (
	navHeight = 34
	navGap    = 2
)

func (v *View) sidebar(c *ui.Context, k pal) {
	bar := c.TitleBar()
	ui.Column(c).Width(172).Background(k.side).Cache().Padding(max(bar.Height+8, 22), 10, 18, 14).Children(func() {
		ui.Box(c.Key("drag")).Absolute().Top(0).Left(0).Right(0).Height(max(bar.Height, 12)).DragWindow()
		ui.Row(c.Key("brand")).Gap(9).AlignItems(ui.Center).Padding(0, 6, 16, 6).Children(func() {
			brandMark(c, 26)
			ui.Text(c, "Moko").FontSize(15).FontWeight(800)
		})
		list := ui.Column(c.Key("nav")).Gap(navGap)
		at := list.AnimateTo("at", float32(v.Page), slide)
		list.Draw(func(p *ui.Painter, r ui.Rect) {
			p.Fill(ui.Rect{X: r.X, Y: r.Y + at*(navHeight+navGap), W: r.W, H: navHeight}, k.navOn(), 10)
		}).Children(func() {
			v.nav(c.Key("n0"), k, PageOverview, icAir, i18n.T("概览", "概要", "Overview"))
			v.nav(c.Key("n1"), k, PageWeather, icCloudSun, i18n.T("天气", "天気", "Weather"))
			v.nav(c.Key("n2"), k, PageTrends, icChart, i18n.T("趋势", "推移", "Trends"))
			v.nav(c.Key("n3"), k, PageEnergy, icBolt, i18n.T("能耗", "電力", "Energy"))
			v.nav(c.Key("n4"), k, PageHome, icHome, i18n.T("Apple 家庭", "Apple ホーム", "Apple Home"))
			v.nav(c.Key("n5"), k, PageSettings, icSettings, i18n.T("设置", "設定", "Settings"))
		})
		ui.Spacer(c)
		v.deviceRow(c.Key("device"), k)
	})
}

// navOn is what the page in view has under it in the sidebar.
func (k pal) navOn() ui.Color {
	if k.dark {
		return k.card2
	}
	return k.line
}

func (v *View) nav(c *ui.Context, k pal, page Page, ic *ui.SVG, label string) {
	on := v.Page == page
	row := ui.ButtonBase(c).Height(navHeight).Padding(0, 10).Gap(9).Radius(10).AlignItems(ui.Center).Justify(ui.Start).FocusRing(false).Label(label)
	if row.Clicked() {
		v.Page = page
		on = true
	}
	ink := k.muted
	bg := ui.Transparent
	switch {
	case on:
		ink = k.text
	case row.Hovered():
		ink, bg = k.text, k.hover
	}
	ink = row.AnimateColor("ink", ink, ui.Animation{Duration: 140 * time.Millisecond})
	press(row).Background(row.AnimateColor("bg", bg, ui.Animation{Duration: 120 * time.Millisecond})).Children(func() {
		ui.Icon(c, ic).FontSize(16).TextColor(ink)
		ui.Text(c, label).FontSize(13).FontWeight(600).NoWrap().TextColor(ink)
	})
}

// deviceRow says at the foot of the sidebar whether the device can be reached: its dot
// does, and the word is there for pointing at it and for assistive technology.
func (v *View) deviceRow(c *ui.Context, k pal) {
	state, dot := i18n.T("搜索中", "検索中", "Searching"), k.faint
	switch v.M.Snap.Phase {
	case device.PhaseOnline:
		state, dot = i18n.T("在线", "接続中", "Online"), k.ok
	case device.PhaseOffline:
		state, dot = i18n.T("离线", "オフライン", "Offline"), k.warn
	}
	ui.Row(c).Padding(0, 10).Height(26).Gap(7).AlignItems(ui.Center).Label("KI-SX100 · " + state).Tooltip(state).Children(func() {
		mark := ui.Box(c).Size(7, 7).Radius(3.5)
		mark.Background(mark.AnimateColor("dot", dot, ui.Animation{Duration: 400 * time.Millisecond}))
		ui.Text(c, "KI-SX100").FontSize(11.5).FontWeight(600).TextColor(k.muted).NoWrap()
	})
}
