package view

import (
	"fmt"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/qzz0518/moko/internal/device"
	"github.com/qzz0518/moko/internal/i18n"
)

// Flyout sizes.
const (
	FlyoutWidth  = 320
	FlyoutHeight = 436
)

// Flyout is the panel of the tray icon: everything for daily use, without opening
// the main window.
func (v *View) Flyout(c *ui.Context) {
	k := v.setup(c)
	m := v.M
	// Its own background rather than the popover's material, so that what is on it keeps
	// its contrast whatever is behind the panel.
	ui.Column(c).Fill().Background(k.bg).TextColor(k.text).FontSize(13).Lang(langTag()).Padding(14).Gap(12).Children(func() {
		if !m.Snap.HasState {
			v.flyoutSearching(c.Key("searching"), k)
		} else {
			v.powerRow(c.Key("power"), k)
			v.flyoutSky(c.Key("sky"))
			v.modeGrid(c.Key("modes"), k)
			v.humidifyRow(c.Key("humidify"), k)
		}
		ui.Spacer(c)
		ui.Row(c.Key("foot")).AlignItems(ui.Center).Gap(8).Children(func() {
			if m.Snap.Phase == device.PhaseOffline {
				ui.Icon(c, icWifiOff).FontSize(13).TextColor(k.warn)
				ui.Text(c, i18n.T("连接中断，正在重连", "再接続中", "Reconnecting")).FontSize(11.5).TextColor(k.warn)
			}
			ui.Spacer(c)
			open := ui.ButtonBase(c.Key("open")).Padding(5, 9).Gap(5).Radius(9).AlignItems(ui.Center).FocusRing(false).Label(i18n.T("打开主窗口", "メインウインドウを開く", "Open main window"))
			if open.Clicked() && m.OpenMain != nil {
				m.OpenMain()
			}
			bg := ui.Transparent
			if open.Hovered() {
				bg = k.hover
			}
			press(open).Background(open.AnimateColor("bg", bg, ui.Animation{Duration: 120 * time.Millisecond})).Children(func() {
				ui.Text(c, i18n.T("打开主窗口", "ウインドウを開く", "Open window")).FontSize(11.5).FontWeight(600).TextColor(k.muted).NoWrap()
				ui.Icon(c, icOpen).FontSize(12).TextColor(k.muted)
			})
		})
	})
}

func (v *View) flyoutSearching(c *ui.Context, k pal) {
	ui.Column(c).Grow(1).MinHeight(180).Center().Gap(12).Children(func() {
		mokoTile(c.Key("moko"), 96, 0.55)
		ui.Text(c, i18n.T("正在寻找你的净化器", "空気清浄機を探しています", "Looking for your purifier")).FontSize(13.5).FontWeight(700).TextColor(k.muted)
	})
}

// flyoutSky is the air, as in the main window, smaller.
func (v *View) flyoutSky(c *ui.Context) {
	a := v.airNow()
	st := v.M.Snap.State
	box, fg := skyBox(c, a, 16)
	skyDecor(c, box, a.mood, fg)
	box.Height(140).Children(func() {
		mascot(c.Key("moko"), 140, a.mood, "Moko", v.kickOf()).Absolute().Right(-8).Bottom(-8)
		ui.Column(c.Key("words")).Fill().Padding(14, 16).Children(func() {
			ui.Text(c, a.title).FontSize(13.5).FontWeight(700).SingleLine()
			ui.Spacer(c)
			ui.Row(c.Key("pm")).AlignItems(ui.End).Gap(5).Children(func() {
				roll(c, a.value).FontSize(52).FontWeight(800).LineHeight(1).LetterSpacing(-1).FontFeatures("tnum")
				ui.Text(c, "PM2.5").FontSize(11).FontWeight(800).Margin(0, 0, 8, 0)
			})
			ui.Row(c.Key("reads")).Gap(10).Margin(4, 0, 0, 0).Children(func() {
				temp, hum, watts := "—", "—", "—"
				if st.SensorsOK && st.Sensors.TempOK {
					temp = fmt.Sprintf("%d°", st.Sensors.TempC)
				}
				if st.SensorsOK && st.Sensors.HumidityOK {
					hum = fmt.Sprintf("%d%%", st.Sensors.Humidity)
				}
				if st.WattsOK {
					watts = fmt.Sprintf("%d W", st.Watts)
				}
				for i, s := range [...]string{temp, hum, watts} {
					ui.Text(c.Key(i), s).FontSize(12.5).FontWeight(700).FontFeatures("tnum").NoWrap().ContentTransition(ui.ContentTransition{})
				}
			})
		})
	})
}
