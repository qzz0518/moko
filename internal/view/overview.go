package view

import (
	"fmt"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/qzz0518/moko/internal/device"
	"github.com/qzz0518/moko/internal/history"
	"github.com/qzz0518/moko/internal/i18n"
	"github.com/qzz0518/moko/internal/sharp"
)

// topCardHeight is the height of the two cards at the top of the overview.
const topCardHeight = 264

func (v *View) overview(c *ui.Context, k pal) {
	if v.M.Snap.Phase == device.PhaseOffline {
		banner(c.Key("offline"), k, k.warn, icWifiOff, fmt.Sprintf(i18n.T("连接中断，正在重连 · 读数更新于 %s", "接続が切れました。再接続中 · 更新 %s", "Connection lost, reconnecting · updated %s"), ago(v.Now().Sub(v.M.Snap.UpdatedAt))))
	}
	if v.M.Snap.State.Fault {
		banner(c.Key("fault"), k, k.bad, icAlert, i18n.T("设备报告了故障，请查看机身面板", "本体に異常があります。本体の表示を確認してください", "The device reports a fault. Check its panel."))
	}
	ui.Row(c.Key("top")).Gap(12).Children(func() {
		v.hero(c.Key("hero"), k)
		v.controls(c.Key("controls"), k)
	})
	v.timeline(c.Key("timeline"), k)
}

func banner(c *ui.Context, k pal, col ui.Color, ic *ui.SVG, text string) {
	ui.Row(c).Padding(9, 14).Gap(9).Radius(12).AlignItems(ui.Center).Background(col.Alpha(0.14)).Children(func() {
		ui.Icon(c, ic).FontSize(15).TextColor(col)
		ui.Text(c, text).FontSize(12.5).TextColor(k.text).Grow(1)
	})
}

// air is what the hero shows of the air and of Moko.
type air struct {
	value, title, hint string
	sky                sky
	mood               mood
}

// noWater is whether the device says its tank is empty.
func (v *View) noWater() bool {
	st := v.M.Snap.State
	return st.StatusOK && (st.Humidifier == sharp.HumidifyNoWater || !st.WaterOK)
}

// airNow reads the device for the hero: its sky, its words and Moko's mood.
func (v *View) airNow() air {
	st := v.M.Snap.State
	on := v.M.on()
	mode := v.M.mode()
	a := air{value: "—", mood: mood{eyes: 1, smile: 0.3, breath: 3600 * time.Millisecond}}
	switch mode {
	case sharp.ModeSilent, sharp.ModeNight:
		a.mood.breath = 5200 * time.Millisecond
	case sharp.ModeHigh, sharp.ModeBoost, sharp.ModeDroplet:
		a.mood.breath = 2200 * time.Millisecond
	}
	humidifying := on && v.M.humidify()
	a.mood.dry = humidifying && v.noWater()
	a.mood.drop = humidifying && !a.mood.dry

	known := st.SensorsOK && st.Sensors.PM25OK
	if known {
		a.value = fmt.Sprint(st.Sensors.PM25)
	}
	switch {
	case !on:
		a.title = i18n.T("Moko 睡着了", "Moko はおやすみ中", "Moko is asleep")
		a.hint = i18n.T("已关机，打开电源叫醒它", "停止中です。電源を入れると起きます", "Turned off. Switch it on to wake it")
		a.sky = nightSky
		a.mood.eyes, a.mood.asleep, a.mood.smile = 0, true, 0.35
	case !known:
		a.title = i18n.T("暂无读数", "測定値なし", "No reading")
		a.hint = i18n.T("传感器还没有数据", "センサーの値を待っています", "Waiting for the sensor")
		a.sky = unknownSky
	default:
		level := airLevel(st.Sensors.PM25)
		a.title, a.hint = airLevelName(level), airLevelHint(level)
		a.sky = airSkies[level]
		a.mood.smile = [...]float32{1, 0.55, 0.1, -0.45, -1}[level]
		a.mood.dust = [...]float32{0, 0, 0.1, 0.5, 0.9}[level]
		if mode == sharp.ModeNight {
			a.hint = i18n.T("夜间模式，安静运行", "おやすみ運転中", "Night mode, running quietly")
			a.sky = nightSky
			a.mood.eyes, a.mood.asleep = 0, true
			a.mood.smile = min(a.mood.smile, 0.45) // it does not laugh in its sleep
		}
	}
	if a.mood.dry {
		a.mood.smile = min(a.mood.smile, -0.2)
	}
	if st.Polluted && on && a.hint == "" {
		a.hint = i18n.T("检测到污染", "汚れを検知しています", "Pollution detected")
	}
	a.mood.shade, a.mood.deep = a.sky.shade, a.sky.deep
	return a
}

// skyFade is how the sky turns when the air changes.
var skyFade = ui.Animation{Duration: 900 * time.Millisecond, Ease: ui.EaseInOut}

// skyBox is the card of the sky that Moko leans into, its color turning with the air.
func skyBox(c *ui.Context, a air, radius float32) (ui.Element, ui.Color) {
	box := ui.Box(c).Radius(radius).Clip()
	fg := box.AnimateColor("fg", a.sky.fg, skyFade)
	box.Background(box.AnimateColor("bg", a.sky.bg, skyFade)).TextColor(fg)
	return box, fg
}

// kickOf is what makes Moko hop when it changes: what the user just did to the device.
func (v *View) kickOf() string {
	return fmt.Sprint(v.M.on(), v.M.mode(), v.M.humidify())
}

func (v *View) hero(c *ui.Context, k pal) {
	a := v.airNow()
	box, fg := skyBox(c, a, 20)
	skyDecor(c, box, a.mood, fg)
	box.Grow(1).Basis(0).MinWidth(0).Height(topCardHeight).Children(func() {
		// In the corner it leans out of, as on the icon.
		mascot(c.Key("moko"), 296, a.mood, "Moko", v.kickOf()).Absolute().Right(-14).Bottom(-14)
		ui.Column(c.Key("words")).Fill().Padding(22, 24).Children(func() {
			ui.Text(c, a.title).FontSize(17).FontWeight(700)
			if a.hint != "" {
				ui.Text(c, a.hint).FontSize(12.5).FontWeight(600).Margin(3, 0, 0, 0)
			}
			v.climate(c.Key("climate"))
			ui.Spacer(c)
			size := float32(100)
			if len(a.value) > 2 { // three digits, to keep clear of Moko
				size = 86
			}
			if a.hint != "" { // a line more above it
				size -= 10
			}
			roll(c, a.value).FontSize(size).FontWeight(800).LineHeight(0.95).LetterSpacing(-3).FontFeatures("tnum")
			// Up into the room a figure's line leaves under it.
			ui.Row(c.Key("unit")).Gap(5).AlignItems(ui.End).Margin(-14, 0, 0, 3).Children(func() {
				ui.Text(c, "PM2.5").FontSize(13).FontWeight(800)
				ui.Text(c, "µg/m³").FontSize(12).FontWeight(600)
			})
		})
	})
}

// modeRows is what the app offers, the rows of one control, as the device has one mode.
var modeRows = [2][4]sharp.Mode{
	{sharp.ModeAuto, sharp.ModeNight, sharp.ModePollen, sharp.ModeBoost},
	{sharp.ModeDroplet, sharp.ModeSilent, sharp.ModeMedium, sharp.ModeHigh},
}

// A mode's tile.
const (
	modeCellHeight = 56
	modeCellGap    = 6
	modeCellRadius = 12
)

// modeIcon is the icon of a mode the app offers. The fan's three speeds go from a feather
// to a tornado.
func modeIcon(mode sharp.Mode) *ui.SVG {
	switch mode {
	case sharp.ModeNight:
		return icMoon
	case sharp.ModePollen:
		return icFlower
	case sharp.ModeBoost:
		return icBoost
	case sharp.ModeDroplet:
		return icDroplets
	case sharp.ModeSilent:
		return icFeather
	case sharp.ModeMedium:
		return icWind
	case sharp.ModeHigh:
		return icTornado
	}
	return icGauge
}

// modeGrid chooses the device's mode, from a tile for each. The color of the one chosen
// goes wherever the mode is: to another tile, up or down too. An off device, or a mode the
// app does not offer, has none chosen. Pick runs only for a click on a mode the device is
// not in.
func (v *View) modeGrid(c *ui.Context, k pal) {
	m := v.M
	disabled := !m.reachable()
	mode := m.mode()
	if !m.on() {
		mode = 0 // the mode an off device remembers is not running
	}
	col, row := -1, -1
	for y, modes := range modeRows {
		for x, candidate := range modes {
			if candidate == mode {
				col, row = x, y
			}
		}
	}
	track := ui.Column(c.Key("modes")).Gap(modeCellGap).Role(ui.RoleRadioGroup).Label(i18n.T("模式", "モード", "Mode"))
	shown := track.AnimateTo("shown", b2f(col >= 0), ui.Animation{Duration: 160 * time.Millisecond})
	type cell struct{ x, y int }
	last := ui.Local(track, "last", func() cell { return cell{max(col, 0), max(row, 0)} })
	if col >= 0 {
		if shown < 0.02 {
			track.Jump("x", float32(col)) // it appears there, rather than flying in
			track.Jump("y", float32(row))
		}
		*last = cell{col, row}
	}
	atX := track.AnimateTo("x", float32(last.x), slide)
	atY := track.AnimateTo("y", float32(last.y), slide)
	track.Draw(func(p *ui.Painter, r ui.Rect) {
		// The tiles are painted here, under what the buttons hold, for the chosen color to
		// slide between them.
		w := (r.W - 3*modeCellGap) / 4
		stepX, stepY := w+modeCellGap, float32(modeCellHeight+modeCellGap)
		for y := range modeRows {
			for x := range modeRows[y] {
				p.Fill(ui.Rect{X: r.X + float32(x)*stepX, Y: r.Y + float32(y)*stepY, W: w, H: modeCellHeight}, k.card2, modeCellRadius)
			}
		}
		if shown > 0.01 {
			p.Fill(ui.Rect{X: r.X + atX*stepX, Y: r.Y + atY*stepY, W: w, H: modeCellHeight}, k.accent.Alpha(shown), modeCellRadius)
		}
	}).Children(func() {
		for y, modes := range modeRows {
			ui.Row(c.Key(y)).Height(modeCellHeight).Gap(modeCellGap).Children(func() {
				for x, mo := range modes {
					on := x == col && y == row
					s := ui.ButtonBase(c.Key(x)).Grow(1).Basis(0).MinWidth(0).Radius(modeCellRadius).Center().FocusRing(false).
						Role(ui.RoleRadio).Checked(on).Label(modeName(mo)).Disabled(disabled)
					if s.Clicked() && !on && !disabled && m.SetMode != nil {
						m.SetMode(mo)
					}
					ink, bg := k.muted, ui.Transparent
					switch {
					case on:
						ink = k.onAccent
					case s.Hovered() && !disabled:
						ink, bg = k.text, k.hover
					}
					quick := ui.Animation{Duration: 140 * time.Millisecond}
					ink = s.AnimateColor("ink", ink, quick)
					if disabled {
						s.Opacity(0.5)
					} else {
						press(s)
					}
					s.Background(s.AnimateColor("bg", bg, quick)).Children(func() {
						ui.Column(c).Gap(4).AlignItems(ui.Center).Children(func() {
							ui.Icon(c, modeIcon(mo)).FontSize(18).TextColor(ink)
							ui.Text(c, modeName(mo)).FontSize(11.5).FontWeight(600).TextColor(ink).NoWrap()
						})
					})
				}
			})
		}
	})
}

// rowIcon starts a row of the controls, in color while what it stands for is on.
func rowIcon(c *ui.Context, k pal, ic *ui.SVG, tint ui.Color, on bool) {
	box := ui.Box(c).Size(32, 32).Radius(10).Center()
	bg, ink := k.card2, k.muted
	if on {
		bg, ink = tint.Alpha(0.16), tint
	}
	fade := ui.Animation{Duration: 160 * time.Millisecond}
	ink = box.AnimateColor("ink", ink, fade)
	box.Background(box.AnimateColor("bg", bg, fade)).Children(func() {
		ui.Icon(c, ic).FontSize(17).TextColor(ink)
	})
}

// powerRow says whether the device runs, and switches it.
func (v *View) powerRow(c *ui.Context, k pal) {
	m := v.M
	title := i18n.T("运行中", "運転中", "Running")
	sub := ""
	if !m.on() {
		title, sub = i18n.T("已关机", "停止中", "Off"), i18n.T("选择模式即可开机", "モードを選ぶと運転します", "Pick a mode to turn on")
	}
	ui.Row(c).AlignItems(ui.Center).Gap(10).Children(func() {
		rowIcon(c.Key("icon"), k, icPower, k.ink, m.on())
		ui.Column(c).Grow(1).Basis(0).MinWidth(0).Gap(1).Children(func() {
			ui.Text(c, title).FontSize(15).FontWeight(700)
			if sub != "" {
				ui.Text(c, sub).FontSize(11.5).TextColor(k.muted)
			}
		})
		want := m.on()
		if ui.Switch(c.Key("power"), &want).Label(i18n.T("电源", "電源", "Power")).Disabled(!m.reachable()).Changed() && m.SetPower != nil {
			m.SetPower(want)
		}
	})
}

func (v *View) controls(c *ui.Context, k pal) {
	card(c, k).Cache().Width(296).Height(topCardHeight).Padding(18).Gap(12).Children(func() {
		v.powerRow(c.Key("power"), k)
		v.modeGrid(c.Key("grid"), k)
		ui.Column(c.Key("foot")).Grow(1).Justify(ui.End).Gap(12).Children(func() {
			ui.Box(c.Key("line")).Height(1).Background(k.line)
			v.humidifyRow(c.Key("humidify"), k)
		})
	})
}

func (v *View) humidifyRow(c *ui.Context, k pal) {
	m := v.M
	note := v.humidifyNote(k)
	on := m.on() && m.humidify()
	ic, tint := icDrop, k.water
	if on && v.noWater() {
		ic, tint = icDropOff, k.warn
	}
	ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
		rowIcon(c.Key("icon"), k, ic, tint, on)
		ui.Column(c).Grow(1).Basis(0).MinWidth(0).Gap(1).Children(func() {
			ui.Text(c, i18n.T("加湿", "加湿", "Humidify")).FontSize(15).FontWeight(700)
			ui.Text(c, note.text).FontSize(11.5).TextColor(note.color).SingleLine()
		})
		want := m.humidify() && m.on()
		if ui.Switch(c.Key("humidify"), &want).Label(i18n.T("加湿", "加湿", "Humidify")).Disabled(!m.reachable()).Changed() && m.SetHumidify != nil {
			m.SetHumidify(want)
		}
	})
}

type humidifyText struct {
	text  string
	color ui.Color
}

func (v *View) humidifyNote(k pal) humidifyText {
	st := v.M.Snap.State
	noWater := v.noWater()
	switch {
	case !v.M.on() || !v.M.humidify():
		return humidifyText{i18n.T("未加湿", "加湿オフ", "Off"), k.muted}
	case noWater:
		return humidifyText{i18n.T("缺水，请给水箱加水", "給水してください", "Refill the tank"), k.warn}
	case st.StatusOK && st.Humidifier == sharp.HumidifyActive:
		return humidifyText{i18n.T("加湿中 · 水箱有水", "加湿中 · 水あり", "Humidifying · water OK"), k.muted}
	case st.StatusOK:
		return humidifyText{i18n.T("待机 · 水箱有水", "待機中 · 水あり", "Standing by · water OK"), k.muted}
	}
	return humidifyText{i18n.T("已开启", "オン", "On"), k.muted}
}

// tile is a figure on a card of its own.
func tile(c *ui.Context, k pal, label, value, unit, note string) {
	card(c, k).Grow(1).Basis(0).MinWidth(0).Padding(14, 16).Gap(4).Children(func() {
		ui.Text(c, label).FontSize(12).FontWeight(600).TextColor(k.muted).SingleLine()
		ui.Row(c).AlignItems(ui.End).Gap(3).Children(func() {
			roll(c, value).FontSize(26).FontWeight(800).FontFeatures("tnum").LineHeight(1.15).SingleLine()
			if unit != "" {
				ui.Text(c, unit).FontSize(12).FontWeight(600).TextColor(k.muted).Margin(0, 0, 4, 0)
			}
		})
		if note != "" {
			ui.Text(c, note).FontSize(11).TextColor(k.faint).SingleLine()
		}
	})
}

// todayWh is the energy used since midnight, or -1 before anything is known.
func (v *View) todayWh() int64 {
	now := v.Now()
	y, mo, d := now.Date()
	midnight := time.Date(y, mo, d, 0, 0, 0, 0, now.Location())
	days := history.Daily(v.M.History, midnight, history.Energy)
	if len(days) == 0 {
		if len(v.M.History) >= 2 {
			return 0
		}
		return -1
	}
	return days[len(days)-1].Amount
}

// stat is a reading over the overview's chart, and the choice of it.
type stat struct {
	metric metric
	label  string
	value  string
	unit   string
}

func (v *View) stats() []stat {
	st := v.M.Snap.State
	pm, hum, temp, watts := "—", "—", "—", "—"
	if st.SensorsOK && st.Sensors.PM25OK {
		pm = fmt.Sprint(st.Sensors.PM25)
	}
	if st.SensorsOK && st.Sensors.HumidityOK {
		hum = fmt.Sprint(st.Sensors.Humidity)
	}
	if st.SensorsOK && st.Sensors.TempOK {
		temp = fmt.Sprint(st.Sensors.TempC)
	}
	if st.WattsOK {
		watts = fmt.Sprint(st.Watts)
	}
	power := i18n.T("功率", "消費電力", "Power") + " · "
	if today := v.todayWh(); today >= 0 {
		val, unit := energyText(today)
		power += fmt.Sprintf(i18n.T("今日 %s %s", "今日 %s %s", "%s %s today"), val, unit)
	} else {
		power += i18n.T("正在统计", "集計中", "Counting")
	}
	return []stat{
		{metricPM25, "PM2.5", pm, metricPM25.unit},
		{metricHumidity, metricHumidity.name(), hum, metricHumidity.unit},
		{metricTemp, metricTemp.name(), temp, metricTemp.unit},
		{metricWatts, power, watts, metricWatts.unit},
	}
}

// timeline is the readings of the moment, each of which shows its last 24 hours when
// chosen: the readings and the choice of them are the same thing.
func (v *View) timeline(c *ui.Context, k pal) {
	stats := v.stats()
	if v.metric >= len(stats) {
		v.metric = 0
	}
	m := stats[v.metric].metric
	card(c, k).Cache().Grow(1).MinHeight(150).Padding(6, 20, 12, 20).Gap(10).Children(func() {
		tabs := ui.Row(c.Key("tabs")).Role(ui.RoleRadioGroup).Label(i18n.T("读数", "測定値", "Readings"))
		at := tabs.AnimateTo("at", float32(v.metric), slide)
		col := tabs.AnimateColor("col", m.color(k), ui.Animation{Duration: 200 * time.Millisecond})
		n := float32(len(stats))
		tabs.Draw(func(p *ui.Painter, r ui.Rect) {
			w := r.W / n
			p.Fill(ui.Rect{X: r.X, Y: r.Y + r.H - 1, W: r.W, H: 1}, k.line, 0)
			p.Fill(ui.Rect{X: r.X + at*w, Y: r.Y + r.H - 2.5, W: w - 16, H: 2.5}, col, 1.25)
		}).Children(func() {
			for i, s := range stats {
				on := i == v.metric
				b := ui.ButtonBase(c.Key(i)).Grow(1).Basis(0).MinWidth(0).Padding(10, 0, 12, 0).Justify(ui.Start).FocusRing(false).
					Role(ui.RoleRadio).Checked(on).Label(s.label)
				if b.Clicked() {
					v.metric = i
				}
				ink := k.muted
				if on || b.Hovered() {
					ink = k.text
				}
				ink = b.AnimateColor("ink", ink, ui.Animation{Duration: 140 * time.Millisecond})
				b.Children(func() {
					ui.Column(c).Grow(1).MinWidth(0).Gap(2).Children(func() {
						ui.Text(c, s.label).FontSize(11.5).FontWeight(600).TextColor(k.muted).SingleLine()
						ui.Row(c).AlignItems(ui.End).Gap(3).Children(func() {
							roll(c, s.value).FontSize(26).FontWeight(800).FontFeatures("tnum").LineHeight(1.1).TextColor(ink)
							ui.Text(c, s.unit).FontSize(11.5).FontWeight(600).TextColor(k.muted).Margin(0, 0, 4, 0)
						})
					})
				})
			}
		})
		bigChart(c, k, "chart", m, v.curveOf(m, 24*time.Hour, 96), "15:04", m.unit)
		axisTimes(c, k, 24*time.Hour, v.Now(), "15:04")
	})
}
