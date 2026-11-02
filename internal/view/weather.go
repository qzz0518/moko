package view

import (
	"fmt"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/qzz0518/moko/internal/i18n"
	"github.com/qzz0518/moko/internal/weather"
)

// staleWeather is how old a report may be and still be shown as the weather now.
const staleWeather = 6 * time.Hour

const openMeteoURL = "https://open-meteo.com/"

// kindName names a kind of weather.
func kindName(k weather.Kind) string {
	switch k {
	case weather.Clear:
		return i18n.T("晴", "晴れ", "Clear")
	case weather.PartlyCloudy:
		return i18n.T("晴间多云", "晴れ時々くもり", "Partly cloudy")
	case weather.Cloudy:
		return i18n.T("阴", "くもり", "Cloudy")
	case weather.Fog:
		return i18n.T("雾", "霧", "Fog")
	case weather.Drizzle:
		return i18n.T("小雨", "小雨", "Drizzle")
	case weather.Rain:
		return i18n.T("雨", "雨", "Rain")
	case weather.Snow:
		return i18n.T("雪", "雪", "Snow")
	}
	return i18n.T("雷雨", "雷雨", "Thunderstorm")
}

// kindIcon is the icon of a kind of weather. Clear skies have a moon at night.
func kindIcon(k weather.Kind, day bool) *ui.SVG {
	switch k {
	case weather.Clear:
		if day {
			return icSun
		}
		return icMoon
	case weather.PartlyCloudy:
		if day {
			return icCloudSun
		}
		return icAir
	case weather.Cloudy:
		return icAir
	case weather.Fog:
		return icFog
	case weather.Drizzle:
		return icDrizzle
	case weather.Rain:
		return icRain
	case weather.Snow:
		return icSnow
	}
	return icThunder
}

// kindTint is the color of a kind of weather.
func kindTint(wk weather.Kind, k pal) ui.Color {
	switch wk {
	case weather.Clear, weather.PartlyCloudy:
		return k.power
	case weather.Drizzle, weather.Rain, weather.Snow, weather.Thunder:
		return k.water
	}
	return k.muted
}

// outdoors is the report to show now, if there is one.
func (v *View) outdoors() *weather.Report {
	r := v.M.Weather
	if v.M.WeatherOff || r == nil || v.Now().Sub(r.At) > staleWeather {
		return nil
	}
	return r
}

// indoors is the device's temperature and humidity as text, and whether it has either.
func (v *View) indoors() (temp, hum string, known bool) {
	st := v.M.Snap.State
	temp, hum = "—", "—"
	if st.SensorsOK && st.Sensors.TempOK {
		temp, known = fmt.Sprintf("%d°C", st.Sensors.TempC), true
	}
	if st.SensorsOK && st.Sensors.HumidityOK {
		hum, known = fmt.Sprintf("%d%%", st.Sensors.Humidity), true
	}
	return temp, hum, known
}

// climate sets inside against outside in the hero, in columns so that the numbers
// compare. Outside goes to the weather page.
func (v *View) climate(c *ui.Context) {
	temp, hum, known := v.indoors()
	r := v.outdoors()
	if !known && r == nil {
		return
	}
	labelWidth := float32(30)
	if i18n.Lang() == "en" {
		labelWidth = 48
	}
	cells := func(ic *ui.SVG, label, temp, hum string) {
		ui.Icon(c, ic).FontSize(14)
		ui.Text(c, label).Width(labelWidth).FontSize(12.5).FontWeight(600).NoWrap()
		ui.Text(c, temp).Width(42).FontSize(13).FontWeight(700).FontFeatures("tnum").NoWrap()
		ui.Text(c, hum).Width(36).FontSize(13).FontWeight(700).FontFeatures("tnum").NoWrap()
	}
	ui.Column(c).Margin(7, 0, 0, -6).AlignSelf(ui.Start).Children(func() {
		if known {
			ui.Row(c.Key("in")).Padding(2, 6).Gap(6).AlignItems(ui.Center).Children(func() {
				cells(icHome, i18n.T("室内", "室内", "Inside"), temp, hum)
			})
		}
		if r == nil {
			return
		}
		outside := i18n.T("室外", "屋外", "Outside")
		outTemp, outHum := fmt.Sprintf("%.0f°C", r.TempC), fmt.Sprintf("%d%%", r.Humidity)
		b := ui.ButtonBase(c.Key("out")).Padding(2, 6).Gap(6).Radius(7).AlignItems(ui.Center).Justify(ui.Start).FocusRing(false).
			Label(fmt.Sprintf("%s %s %s %s", outside, outTemp, outHum, kindName(r.Kind))).
			Tooltip(r.Place.Name + " · " + kindName(r.Kind))
		if b.Clicked() {
			v.Page = PageWeather
		}
		bg := ui.Transparent
		if b.Hovered() {
			bg = ui.RGBA(255, 255, 255, 0.16)
		}
		press(b).Background(b.AnimateColor("bg", bg, ui.Animation{Duration: 120 * time.Millisecond})).Children(func() {
			cells(kindIcon(r.Kind, r.Daytime), outside, outTemp, outHum)
		})
	})
}

func (v *View) weatherPage(c *ui.Context, k pal) {
	m := v.M
	r := m.Weather
	sub := i18n.T("还没有地点", "場所が未設定です", "No place yet")
	if m.Place != "" {
		sub = m.Place
	}
	if r != nil && !m.WeatherOff {
		sub += fmt.Sprintf(i18n.T(" · 更新于 %s", " · %s 更新", " · updated %s"), r.At.Format("15:04"))
	}
	pageTitle(c, k, i18n.T("天气", "天気", "Weather"), sub)
	ui.Scroll(c.Key("scroll")).Grow(1).Gap(12).Children(func() {
		switch {
		case m.WeatherOff:
		case r == nil:
			card(c.Key("wait"), k).Padding(28).Center().Children(func() {
				ui.Text(c, i18n.T("正在读取天气，也可以在下面指定地点", "天気を読み込み中です。下で場所を指定することもできます", "Reading the weather. You can also choose a place below")).FontSize(12.5).TextColor(k.muted)
			})
		default:
			ui.Row(c.Key("top")).Gap(12).AlignItems(ui.Stretch).Children(func() {
				v.weatherNow(c.Key("now"), k, r)
				v.weatherCompare(c.Key("compare"), k, r)
			})
			weatherHours(c.Key("hours"), k, r)
			weatherDays(c.Key("days"), k, r)
		}
		v.weatherSettings(c.Key("settings"), k)
	})
}

func (v *View) weatherNow(c *ui.Context, k pal, r *weather.Report) {
	card(c, k).Grow(1).Basis(0).MinWidth(0).Padding(18, 20).Children(func() {
		ui.Row(c).AlignItems(ui.Center).Gap(14).Grow(1).Children(func() {
			ui.Column(c).Grow(1).MinWidth(0).Gap(2).Children(func() {
				ui.Text(c, kindName(r.Kind)).FontSize(14).FontWeight(700)
				roll(c, fmt.Sprintf("%.0f°", r.TempC)).FontSize(58).FontWeight(800).LineHeight(1.05).LetterSpacing(-1).FontFeatures("tnum")
			})
			ui.Icon(c, kindIcon(r.Kind, r.Daytime)).FontSize(56).TextColor(kindTint(r.Kind, k))
		})
	})
}

// weatherCompare sets outside and inside side by side, which is what they are for.
func (v *View) weatherCompare(c *ui.Context, k pal, r *weather.Report) {
	inTemp, inHum, _ := v.indoors()
	rows := [][3]string{
		{"", i18n.T("室外", "屋外", "Outside"), i18n.T("室内", "室内", "Inside")},
		{i18n.T("温度", "温度", "Temperature"), fmt.Sprintf("%.0f°C", r.TempC), inTemp},
		{i18n.T("湿度", "湿度", "Humidity"), fmt.Sprintf("%d%%", r.Humidity), inHum},
	}
	card(c, k).Grow(1).Basis(0).MinWidth(0).Padding(14, 18).Gap(10).Justify(ui.Center).Children(func() {
		for i, row := range rows {
			ui.Row(c.Key(i)).AlignItems(ui.Center).Children(func() {
				ui.Text(c, row[0]).FontSize(12.5).TextColor(k.muted).Grow(1).MinWidth(0).SingleLine()
				for j, s := range row[1:] {
					t := ui.Text(c.Key(j), s).Width(72).TextAlign(ui.End).FontSize(15).FontWeight(700).FontFeatures("tnum").NoWrap()
					if i == 0 {
						t.FontSize(11.5).FontWeight(600).TextColor(k.muted)
					}
				}
			})
		}
	})
}

func weatherHours(c *ui.Context, k pal, r *weather.Report) {
	if len(r.Hours) == 0 {
		return
	}
	card(c, k).Padding(14, 12).Children(func() {
		ui.Row(c).Children(func() {
			for i, h := range r.Hours {
				ui.Column(c.Key(i)).Grow(1).Basis(0).MinWidth(0).Gap(5).AlignItems(ui.Center).Children(func() {
					ui.Text(c, h.At.Format("15:04")).FontSize(11.5).FontFeatures("tnum").TextColor(k.muted)
					day := h.At.Hour() >= 6 && h.At.Hour() < 18
					ui.Icon(c, kindIcon(h.Kind, day)).FontSize(20).TextColor(kindTint(h.Kind, k))
					ui.Text(c, fmt.Sprintf("%.0f°", h.TempC)).FontSize(13).FontWeight(700).FontFeatures("tnum")
				})
			}
		})
	})
}

func weekdayName(d time.Weekday) string {
	return [...]string{
		i18n.T("周日", "日曜", "Sun"), i18n.T("周一", "月曜", "Mon"), i18n.T("周二", "火曜", "Tue"), i18n.T("周三", "水曜", "Wed"),
		i18n.T("周四", "木曜", "Thu"), i18n.T("周五", "金曜", "Fri"), i18n.T("周六", "土曜", "Sat"),
	}[d]
}

func weatherDays(c *ui.Context, k pal, r *weather.Report) {
	if len(r.Days) == 0 {
		return
	}
	ui.Row(c).Gap(12).Children(func() {
		for i, d := range r.Days {
			card(c.Key(i), k).Grow(1).Basis(0).MinWidth(0).Padding(13, 16).Children(func() {
				ui.Row(c).AlignItems(ui.Center).Gap(11).Children(func() {
					ui.Icon(c, kindIcon(d.Kind, true)).FontSize(24).TextColor(kindTint(d.Kind, k))
					ui.Column(c).Grow(1).MinWidth(0).Gap(1).Children(func() {
						ui.Text(c, weekdayName(d.Date.Weekday())).FontSize(12).TextColor(k.muted)
						ui.Text(c, fmt.Sprintf("%.0f° / %.0f°", d.MaxC, d.MinC)).FontSize(13.5).FontWeight(700).FontFeatures("tnum").NoWrap()
					})
					if d.RainPct >= 0 {
						ui.Text(c, fmt.Sprintf("%d%%", d.RainPct)).FontSize(12).FontWeight(600).FontFeatures("tnum").TextColor(k.water).NoWrap().Tooltip(i18n.T("降水概率", "降水確率", "Chance of rain"))
					}
				})
			})
		}
	})
}

// weatherSettings turns the weather on or off, and says where.
func (v *View) weatherSettings(c *ui.Context, k pal) {
	m := v.M
	card(c, k).Children(func() {
		settingRow(c.Key("on"), k, i18n.T("显示室外天气", "屋外の天気を表示", "Show the weather outside"), i18n.T("会访问网络，其余功能都只走局域网", "インターネットを使います。ほかの機能はローカルネットワークのみです", "Uses the internet; everything else stays on the local network"), func() {
			want := !m.WeatherOff
			if ui.Switch(c.Key("on"), &want).Label(i18n.T("显示室外天气", "屋外の天気を表示", "Show the weather outside")).Changed() && m.SetWeatherOn != nil {
				m.SetWeatherOn(want)
			}
		})
		if m.WeatherOff {
			return
		}
		hairline(c, k)
		note := i18n.T("按 IP 地址估算，开 VPN 时可能不准", "IP アドレスから推定します。VPN 使用中は不正確な場合があります", "Estimated from your IP address, which a VPN can throw off")
		if m.PlaceFixed {
			note = i18n.T("你选的地点", "選択した場所", "The place you chose")
		}
		settingRow(c.Key("place"), k, i18n.T("地点", "場所", "Place"), note, func() {
			ui.Row(c.Key("controls")).Gap(6).AlignItems(ui.Center).Children(func() {
				if m.PlaceFixed {
					smallButton(c.Key("auto"), k, i18n.T("改回自动", "自動に戻す", "Use automatic"), nil, func() {
						v.placeQuery = ""
						if m.ChoosePlace != nil {
							m.ChoosePlace(nil)
						}
					})
				}
				in := ui.TextInput(c.Key("input"), &v.placeQuery).Width(controlWidth).Placeholder(i18n.T("搜索城市，回车", "都市を検索", "Search a city")).Label(i18n.T("搜索城市", "都市を検索", "Search a city"))
				if in.Submitted() && m.SearchPlace != nil {
					m.SearchPlace(v.placeQuery)
				}
			})
		})
		if m.PlaceSearching {
			ui.Text(c.Key("searching"), i18n.T("搜索中…", "検索中…", "Searching…")).FontSize(12).TextColor(k.muted).Padding(0, 16, 12, 16)
		}
		if m.PlaceNote != "" {
			ui.Text(c.Key("note"), m.PlaceNote).FontSize(12).TextColor(k.warn).Padding(0, 16, 12, 16)
		}
		for i, p := range m.Places {
			hairline(c.Key(100+i), k)
			row := ui.ButtonBase(c.Key(200+i)).MinHeight(40).Padding(6, 16).Gap(8).AlignItems(ui.Center).Justify(ui.Start).FocusRing(false).Label(p.Name)
			if row.Clicked() && m.ChoosePlace != nil {
				place := p
				v.placeQuery = ""
				m.ChoosePlace(&place)
			}
			bg := ui.Transparent
			if row.Hovered() {
				bg = k.hover
			}
			row.Background(row.AnimateColor("bg", bg, ui.Animation{Duration: 120 * time.Millisecond})).Children(func() {
				ui.Text(c, p.Name).FontSize(13).FontWeight(600).NoWrap()
				ui.Text(c, p.Detail).FontSize(11.5).TextColor(k.muted).SingleLine()
			})
		}
		hairline(c.Key("source-line"), k)
		settingRow(c.Key("source"), k, i18n.T("数据来源", "データ提供", "Data"), "", func() {
			smallButton(c.Key("meteo"), k, "Open-Meteo", icOpen, func() {
				if m.OpenURL != nil {
					m.OpenURL(openMeteoURL)
				}
			}).Tooltip(openMeteoURL)
		})
	})
}
