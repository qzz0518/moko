package view

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/chart"

	"github.com/qzz0518/moko/internal/history"
	"github.com/qzz0518/moko/internal/i18n"
)

// card is the surface everything sits on: a sheet on the paper, with one hairline for
// its edge and no shadow.
func card(c *ui.Context, k pal) ui.Element {
	return ui.Column(c).Background(k.card).Radius(20).Border(1, k.line)
}

// press makes a control give a little under the pointer.
func press(b ui.Element) ui.Element {
	pose := ui.Pose{}
	if b.Pressed() {
		pose.Scale = 0.96
	}
	return b.AnimatePose(pose, ui.Snappy)
}

// roll shows a number whose digits roll to a new value, up or down as it went.
// settled is on, late enough for the switch that changes it to have finished sliding: a
// page that rearranged at once would carry the knob away in the middle of its slide.
// Turning on takes longer, past switchSlide in main.go: what it turns on has started by
// then, and the page does not go through a state in between. Turning off comes before it.
func settled(e ui.Element, on bool) bool {
	wait := 180 * time.Millisecond
	if on {
		wait = 260 * time.Millisecond
	}
	at := e.AnimateTo("settled", b2f(on), ui.Animation{Duration: wait})
	if on {
		return at >= 0.99
	}
	return at > 0.01
}

func roll(c *ui.Context, s string) ui.Element {
	e := ui.Text(c, s)
	prev := ui.Local(e, "prev", func() string { return s })
	down := ui.Local(e, "down", func() bool { return false })
	if s != *prev {
		was, err1 := strconv.ParseFloat(*prev, 64)
		now, err2 := strconv.ParseFloat(s, 64)
		*down = err1 == nil && err2 == nil && now < was
		*prev = s
	}
	return e.ContentTransition(ui.ContentTransition{Numeric: true, Down: *down})
}

// choice is a segment of a chooser.
type choice struct {
	label string
	on    bool
}

// slide is how a selection moves from one option to another.
var slide = ui.Animation{Spring: ui.SpringOptions{Duration: 300 * time.Millisecond, Bounce: 0.1}}

// chooser is a segmented control in the app's look, whose capsule slides to what is
// chosen. Up to one option is on: the two groups of modes share one selection, so a
// group often has none, and its capsule fades away where it was. ui.SegmentedBase
// cannot show that, as it moves a selection of -1 to the first segment, which here
// would send a command nobody asked for. Pick runs only for a click on an option
// that is not on.
func chooser(c *ui.Context, k pal, key, title string, showTitle bool, opts []choice, disabled bool, pick func(i int)) {
	ui.Column(c.Key(key)).Gap(7).Children(func() {
		if showTitle {
			ui.Text(c, title).FontSize(11.5).FontWeight(600).TextColor(k.faint)
		}
		track := ui.Row(c.Key("track")).Padding(3).Radius(13).Background(k.card2).Role(ui.RoleRadioGroup).Label(title)
		on := -1
		for i, o := range opts {
			if o.on {
				on = i
			}
		}
		last := ui.Local(track, "last", func() int { return max(on, 0) })
		shown := track.AnimateTo("shown", b2f(on >= 0), ui.Animation{Duration: 160 * time.Millisecond})
		if on >= 0 {
			if shown < 0.02 {
				track.Jump("at", float32(on)) // it appears there, rather than flying in
			}
			*last = on
		}
		at := track.AnimateTo("at", float32(*last), slide)
		n := float32(len(opts))
		track.Draw(func(p *ui.Painter, r ui.Rect) {
			if shown <= 0.01 {
				return
			}
			w := (r.W - 6) / n
			pill := ui.Rect{X: r.X + 3 + at*w, Y: r.Y + 3, W: w, H: r.H - 6}
			p.Fill(pill, k.capsule.Alpha(shown), 10)
		}).Children(func() {
			for i, o := range opts {
				s := ui.ButtonBase(c.Key(i)).Grow(1).Basis(0).Height(32).Radius(10).Center().FocusRing(false).
					Role(ui.RoleRadio).Checked(o.on).Label(o.label).Disabled(disabled)
				if s.Clicked() && !o.on && !disabled && pick != nil {
					pick(i)
				}
				ink := k.muted
				switch {
				case o.on:
					ink = k.onCapsule
				case s.Hovered() && !disabled:
					ink = k.text
				}
				ink = s.AnimateColor("ink", ink, ui.Animation{Duration: 140 * time.Millisecond})
				if disabled {
					s.Opacity(0.5)
				} else {
					press(s)
				}
				s.Children(func() {
					t := ui.Text(c, o.label).FontSize(12.5).FontWeight(600).TextColor(ink).NoWrap()
					if o.on {
						t.FontWeight(700)
					}
				})
			}
		})
	})
}

// settingRow is a label with a note on the left and a control on the right.
func settingRow(c *ui.Context, k pal, label, note string, control func()) {
	ui.Row(c).MinHeight(52).Padding(8, 16).Gap(16).AlignItems(ui.Center).Children(func() {
		ui.Column(c).Grow(1).Basis(0).MinWidth(0).Gap(2).Children(func() {
			ui.Text(c, label).FontSize(13).FontWeight(600)
			if note != "" {
				ui.Text(c, note).FontSize(11.5).TextColor(k.muted)
			}
		})
		control()
	})
}

// hairline separates the rows of a card.
func hairline(c *ui.Context, k pal) {
	ui.Box(c).Height(1).Background(k.line).Margin(0, 16)
}

// section is a titled card of rows.
func section(c *ui.Context, k pal, title string, rows func()) {
	ui.Column(c).Gap(8).Children(func() {
		ui.Text(c, title).FontSize(12).FontWeight(600).TextColor(k.muted).Margin(0, 0, 0, 4)
		card(c, k).Children(rows)
	})
}

// metric is a value the charts can show.
type metric struct {
	name  func() string
	unit  string
	color func(k pal) ui.Color
	value func(s history.Sample) int
	lo    float64 // the axis's floor
	span  float64 // the least the axis covers
}

var (
	metricPM25 = metric{
		name: func() string { return "PM2.5" }, unit: "µg/m³",
		color: func(k pal) ui.Color { return k.accent },
		value: func(s history.Sample) int { return s.PM25 }, lo: 0, span: 20,
	}
	metricHumidity = metric{
		name: func() string { return i18n.T("湿度", "湿度", "Humidity") }, unit: "%",
		color: func(k pal) ui.Color { return k.water },
		value: func(s history.Sample) int { return s.Humidity }, lo: math.NaN(), span: 16,
	}
	metricTemp = metric{
		name: func() string { return i18n.T("室温", "室温", "Temperature") }, unit: "°C",
		color: func(k pal) ui.Color { return k.warm },
		value: func(s history.Sample) int { return s.TempC }, lo: math.NaN(), span: 6,
	}
	metricWatts = metric{
		name: func() string { return i18n.T("功率", "消費電力", "Power") }, unit: "W",
		color: func(k pal) ui.Color { return k.power },
		value: func(s history.Sample) int { return s.Watts }, lo: 0, span: 20,
	}
)

// curve is a metric over a stretch of time, ready to draw.
type curve struct {
	xs, ys   []float32 // fractions; a NaN breaks the line
	buckets  []history.Bucket
	lo, hi   float64 // of the axis
	min, max float64
	avg      float64
	count    int
}

type seriesKey struct {
	metric string
	minute int64
	span   time.Duration
	n      int
	length int
}

// seriesCache keeps the curves of the last frames: frames come far more often than
// samples do.
type seriesCache struct{ m map[seriesKey]curve }

func (v *View) curveOf(m metric, span time.Duration, n int) curve {
	now := v.Now()
	key := seriesKey{m.unit, now.Unix() / 60, span, n, len(v.M.History)}
	if got, ok := v.series.m[key]; ok {
		return got
	}
	if v.series.m == nil || len(v.series.m) > 32 {
		v.series.m = map[seriesKey]curve{}
	}
	got := makeCurve(v.M.History, now.Add(-span), now, n, m)
	v.series.m[key] = got
	return got
}

func makeCurve(samples []history.Sample, start, end time.Time, n int, m metric) curve {
	b := history.Buckets(samples, start, end, n, m.value)
	cv := curve{buckets: b, xs: make([]float32, n), ys: make([]float32, n), min: math.Inf(1), max: math.Inf(-1)}
	sum := 0.0
	for _, x := range b {
		if x.Empty {
			continue
		}
		cv.min, cv.max = math.Min(cv.min, x.Value), math.Max(cv.max, x.Value)
		sum += x.Value
		cv.count++
	}
	if cv.count == 0 {
		cv.min, cv.max = 0, 0
		return cv
	}
	cv.avg = sum / float64(cv.count)
	cv.lo, cv.hi = cv.min, cv.max
	if !math.IsNaN(m.lo) {
		cv.lo = m.lo
	}
	if cv.hi-cv.lo < m.span {
		if math.IsNaN(m.lo) {
			mid := (cv.lo + cv.hi) / 2
			cv.lo, cv.hi = mid-m.span/2, mid+m.span/2
		} else {
			cv.hi = cv.lo + m.span
		}
	} else if math.IsNaN(m.lo) {
		pad := (cv.hi - cv.lo) * 0.15
		cv.lo, cv.hi = cv.lo-pad, cv.hi+pad
	} else {
		cv.hi += (cv.hi - cv.lo) * 0.15
	}
	for i, x := range b {
		cv.xs[i] = float32(i) / float32(n-1)
		if x.Empty {
			cv.ys[i] = float32(math.NaN())
			continue
		}
		cv.ys[i] = float32((x.Value - cv.lo) / (cv.hi - cv.lo))
	}
	return cv
}

func fade(col ui.Color, from float32) ui.LinearGradient {
	return ui.LinearGradient{Angle: 180, Stops: []ui.GradientStop{{Color: col.Alpha(from), At: 0}, {Color: col.Alpha(0), At: 1}}}
}

// drawIn is how far a chart has drawn itself since tag, what it shows, last changed.
// A chart just shown is whole; choosing another thing to show draws it from the left.
func drawIn(box ui.Element, tag any) float32 {
	last := ui.Local(box, "tag", func() any { return tag })
	if *last != tag {
		*last = tag
		box.Jump("in", 0)
	}
	return box.AnimateTo("in", 1, ui.Animation{Duration: 600 * time.Millisecond, Ease: ui.CubicBezier(0.32, 0.72, 0, 1)})
}

// bigChart is a soft line with grid lines, and the reading under the pointer.
func bigChart(c *ui.Context, k pal, key string, m metric, cv curve, format string, tag any) {
	col := m.color(k)
	if cv.count < 2 {
		ui.Column(c.Key(key)).Grow(1).MinHeight(80).Center().Children(func() {
			ui.Text(c, i18n.T("正在积累数据，稍后再来看", "データを集めています", "Collecting readings…")).FontSize(12.5).TextColor(k.faint)
		})
		return
	}
	box := ui.Box(c.Key(key)).Grow(1).MinHeight(80)
	in := drawIn(box, tag)
	x, _, hovering := box.PointerPosition()
	box.Draw(func(p *ui.Painter, r ui.Rect) {
		p.Line(r.X, r.Y+r.H, r.X+r.W, r.Y+r.H, 1, k.line)
		p.Clip(ui.Rect{X: r.X - 4, Y: r.Y - 6, W: (r.W + 8) * in, H: r.H + 12}, 0, func() {
			p.FillPathGradient(chart.Area(r, cv.xs, nil, cv.ys, chart.CurveMonotone), fade(col, 0.16))
			p.StrokePath(chart.Line(r, cv.xs, cv.ys, chart.CurveMonotone).Cap(ui.RoundCap), 2.2, col)
		})
		if in < 0.98 {
			return
		}

		i := len(cv.buckets) - 1
		for i > 0 && cv.buckets[i].Empty {
			i--
		}
		if hovering {
			i = nearest(cv, x/r.W) // the pointer is relative to the box
		}
		if cv.buckets[i].Empty {
			return
		}
		dx, dy := r.X+r.W*cv.xs[i], r.Y+r.H*(1-cv.ys[i])
		if hovering {
			p.Line(dx, r.Y, dx, r.Y+r.H, 1, k.border)
			label := ui.Span{Text: fmt.Sprintf("%s  %.0f %s", cv.buckets[i].At.Format(format), cv.buckets[i].Value, m.unit), Size: 11.5, Weight: 700, Color: k.bg, Features: "tnum"}
			w, h := p.MeasureText(0, label)
			tx := min(max(dx-w/2, r.X+8), r.X+r.W-w-8)
			p.Fill(ui.Rect{X: tx - 8, Y: r.Y - 4, W: w + 16, H: h + 8}, k.text, 8)
			p.RichText(tx, r.Y, 0, label)
		}
		var ring, dot ui.Path
		ring.Circle(dx, dy, 6)
		p.FillPath(&ring, k.card)
		dot.Circle(dx, dy, 4)
		p.FillPath(&dot, col)
	})
}

func nearest(cv curve, f float32) int {
	i := int(f*float32(len(cv.buckets)-1) + 0.5)
	return max(0, min(i, len(cv.buckets)-1))
}

// axisTimes labels the time axis under a chart.
func axisTimes(c *ui.Context, k pal, span time.Duration, now time.Time, format string) {
	ui.Row(c).Children(func() {
		const n = 5
		for i := range n {
			at := now.Add(-span + span*time.Duration(i)/(n-1))
			label := at.Format(format)
			if i == n-1 {
				label = i18n.T("现在", "現在", "Now")
			}
			ui.Text(c.Key(100+i), label).FontSize(10.5).TextColor(k.faint).FontFeatures("tnum").NoWrap()
			if i < n-1 {
				ui.Spacer(c.Key(200 + i))
			}
		}
	})
}

// ago says how long since a reading, in the words of the language in use.
func ago(d time.Duration) string {
	switch {
	case d < time.Minute:
		return i18n.T("刚刚", "たった今", "just now")
	case d < time.Hour:
		n := int(d.Minutes())
		return fmt.Sprintf(i18n.T("%d 分钟前", "%d 分前", "%d min ago"), n)
	}
	n := int(d.Hours())
	return fmt.Sprintf(i18n.T("%d 小时前", "%d 時間前", "%d h ago"), n)
}

// energyText shows an amount of energy in the unit it reads best in.
func energyText(wh int64) (value, unit string) {
	if wh < 1000 {
		return fmt.Sprint(wh), "Wh"
	}
	return fmt.Sprintf("%.2f", float64(wh)/1000), "kWh"
}

// yenText shows what an amount of energy costs.
func yenText(wh int64, price float64) string {
	yen := float64(wh) / 1000 * price
	var s string
	if yen < 10 {
		s = fmt.Sprintf("%.1f", yen)
	} else {
		s = thousands(int64(yen + 0.5))
	}
	return fmt.Sprintf(i18n.T("约 %s 日元", "約 %s 円", "about ¥%s"), s)
}

func thousands(n int64) string {
	s := fmt.Sprint(n)
	var b strings.Builder
	for i, r := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(r)
	}
	return b.String()
}
