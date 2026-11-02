package view

import (
	"math"
	"time"

	"github.com/egoist/mygo/ui"
)

// mood is how Moko looks: what it shows is the state of the air and of the device, so
// that a glance tells.
type mood struct {
	smile       float32 // from -1, a frown, to 1, laughing
	eyes        float32 // 1 open, 0 shut
	dust        float32 // how gray the air has made it, up to 1
	drop        bool    // humidifying: a droplet floats over it
	dry         bool    // out of water: the droplet is empty
	asleep      bool
	breath      time.Duration // the fan's pace
	shade, deep ui.Color      // the sky's light on its underside, and its shadow on the sky
}

// The outline of Moko's head, from assets/icon.svg, with the origin between its eyes:
// six lobes, whose joins are rounded, open at the bottom, where whatever it leans out
// of cuts it off. Each line is a cubic segment.
var (
	mokoStart = [2]float32{-510, 700}
	mokoHead  = [...]float32{
		-510.0, 700.0, -500.0, 430.0, -500.0, 430.0,
		-500.0, 369.2, -472.3, 311.7, -424.8, 273.7,
		-417.2, 267.6, -413.0, 258.3, -413.6, 248.5,
		-414.1, 238.8, -419.3, 230.0, -427.6, 224.8,
		-477.1, 193.9, -506.5, 139.0, -504.9, 80.7,
		-503.4, 22.3, -471.2, -30.9, -420.2, -59.2,
		-407.0, -66.5, -401.3, -82.5, -406.9, -96.5,
		-431.9, -159.5, -420.6, -231.1, -377.3, -283.3,
		-333.9, -335.4, -265.6, -359.6, -199.1, -346.5,
		-185.4, -343.8, -171.7, -350.8, -165.9, -363.6,
		-130.1, -442.7, -50.2, -492.6, 36.6, -489.9,
		123.5, -487.2, 200.2, -432.5, 231.0, -351.3,
		236.1, -337.9, 249.9, -330.0, 264.1, -332.4,
		325.0, -342.7, 387.0, -322.0, 429.4, -277.1,
		471.9, -232.2, 489.1, -169.1, 475.4, -108.9,
		472.0, -94.1, 480.3, -79.0, 494.7, -74.0,
		572.8, -46.5, 625.0, 27.2, 625.0, 110.0,
		625.0, 110.0, 625.0, 700.0, 625.0, 700.0,
	}
)

// mokoHeadAt is Moko's head, at s DIPs to a unit of its outline. A path is rasterized at
// the size of its coordinates, whatever transform draws it, into an atlas it shares with
// the text: coordinates in DIPs keep the mask small, and one size of it, which is drawn
// once and moved with transforms, needs no drawing again as Moko breathes.
func mokoHeadAt(s float32) *ui.Path {
	var path ui.Path
	path.MoveTo(mokoStart[0]*s, mokoStart[1]*s)
	for i := 0; i+5 < len(mokoHead); i += 6 {
		path.CubeTo(mokoHead[i]*s, mokoHead[i+1]*s, mokoHead[i+2]*s, mokoHead[i+3]*s, mokoHead[i+4]*s, mokoHead[i+5]*s)
	}
	return path.Close()
}

// dropAt is a droplet around the origin, 200 units tall at z = 1.
func dropAt(z float32) *ui.Path {
	var path ui.Path
	path.MoveTo(0, -100*z).CubeTo(18*z, -62*z, 70*z, -22*z, 70*z, 34*z).CubeTo(70*z, 74*z, 38*z, 104*z, 0, 104*z).
		CubeTo(-38*z, 104*z, -70*z, 74*z, -70*z, 34*z).CubeTo(-70*z, -22*z, -18*z, -62*z, 0, -100*z)
	return path.Close()
}

func ellipse(path *ui.Path, cx, cy, rx, ry float32) {
	const q = 0.5522847498
	path.MoveTo(cx+rx, cy)
	path.CubeTo(cx+rx, cy+ry*q, cx+rx*q, cy+ry, cx, cy+ry)
	path.CubeTo(cx-rx*q, cy+ry, cx-rx, cy+ry*q, cx-rx, cy)
	path.CubeTo(cx-rx, cy-ry*q, cx-rx*q, cy-ry, cx, cy-ry)
	path.CubeTo(cx+rx*q, cy-ry, cx+rx, cy-ry*q, cx+rx, cy)
	path.Close()
}

// Moko's colors, from assets/icon.svg. Its eyes and shadow are the blue of the icon,
// not black, as the shadows of a thing on a blue ground are.
var (
	mokoInk    = ui.Hex("#0A1C63")
	mokoIris   = ui.Hex("#1E46D6")
	mokoBlush  = ui.Hex("#FFC9D6")
	mokoShade  = ui.Hex("#BFD8FF")
	mokoDeep   = ui.Hex("#0B2CA8")
	mokoDropA  = ui.Hex("#E4FCFF")
	mokoDropB  = ui.Hex("#6FE0FF")
	brandFrom  = ui.Hex("#2B8CFF")
	brandTo    = ui.Hex("#1D5EF5")
	brandWind  = ui.Hex("#A9D4FF")
	mokoWhite  = ui.RGB(255, 255, 255)
	mokoDusted = ui.Oklch(0.72, 0.012, 85)
)

// calmMood is Moko when there is no air to speak of, on the blue of the icon.
func calmMood(smile float32) mood {
	return mood{eyes: 1, smile: smile, shade: mokoShade, deep: mokoDeep}
}

// drawMoko paints Moko leaning out of the lower right of r, which is the square of the
// icon, at time t in seconds; dropShown is how far its droplet has appeared. What
// holds r should clip it, and r should reach that clip's corner.
func drawMoko(p *ui.Painter, r ui.Rect, m mood, t float64, hop, dropShown float32) {
	breath := m.breath.Seconds()
	if breath <= 0 {
		breath = 3.6
	}
	b := float32(math.Sin(t / breath * 2 * math.Pi))
	unit := r.W / 1024
	s := unit * 0.98
	lean := ui.Scaling(hop*(1-0.006*b), hop*(1+0.01*b)).Then(ui.Rotation(-15 + 0.5*b)).Then(ui.Translation(r.X+r.W*0.547, r.Y+r.H*0.684))

	p.Transform(lean, 0, 0, func() {
		head := mokoHeadAt(s)
		p.Transform(ui.Translation(-24*s, -20*s), 0, 0, func() {
			p.FillPath(head, m.deep.Alpha(0.34))
		})
		white := mokoWhite.Mix(mokoDusted, m.dust*0.55)
		p.FillPathGradient(head, ui.LinearGradient{Angle: 140, Stops: []ui.GradientStop{
			{Color: white, At: 0}, {Color: white, At: 0.58}, {Color: m.shade.Mix(mokoDusted, m.dust*0.5), At: 0.92},
		}})
		drawFace(p, m, t, s)
	})

	if dropShown > 0.01 {
		z := unit * 0.82 * dropShown
		bob := float32(math.Sin(t*1.7)) * 12 * unit
		p.Transform(ui.Rotation(16).Then(ui.Translation(r.X+r.W*0.838, r.Y+r.H*0.172+bob)), 0, 0, func() {
			drop := dropAt(z)
			if m.dry {
				p.StrokePath(drop, 13*z, mokoWhite.Alpha(0.85))
				return
			}
			p.FillPathGradient(drop, ui.LinearGradient{Angle: 180, Stops: []ui.GradientStop{{Color: mokoDropA, At: 0}, {Color: mokoDropB, At: 1}}})
			var glint ui.Path
			glint.MoveTo(-34*z, 34*z).CubeTo(-34*z, 56*z, -20*z, 70*z, -2*z, 74*z)
			p.StrokePath(glint.Cap(ui.RoundCap), 15*z, mokoWhite)
		})
	}

	if m.asleep {
		for i := range 3 {
			f := float32(math.Mod(t/4.2+float64(i)/3, 1))
			if t == 0 {
				f = 0.2 + 0.28*float32(i)
			}
			size := (70 + f*70) * unit
			p.Text(r.X+r.W*(0.2-f*0.1), r.Y+r.H*(0.26-f*0.2), "z", size, mokoWhite.Alpha(float32(math.Sin(float64(f)*math.Pi))*0.9))
		}
	}
}

// drawFace paints Moko's face around the origin, between its eyes, s DIPs to a unit.
func drawFace(p *ui.Painter, m mood, t float64, s float32) {
	// Blush, which gray air takes away.
	for _, cx := range [2]float32{-277, 213} {
		var cheek ui.Path
		ellipse(&cheek, cx*s, 100*s, 58*s, 32*s)
		p.FillPath(&cheek, mokoBlush.Alpha(1-m.dust*0.8))
	}

	// Eyes, which blink now and then, and get smaller when the air is bad.
	sad := max(-m.smile, 0)
	open := m.eyes
	if !m.asleep {
		if phase := math.Mod(t, 4.3); phase < 0.16 {
			open *= float32(math.Abs(phase/0.08 - 1))
		}
	}
	for i, x := range [2]float32{-150, 86} {
		cx, cy := x*s, -8*s
		p.Transform(ui.Rotation(float32(-3+6*i)), cx, cy, func() {
			if open < 0.16 {
				var arc ui.Path
				arc.MoveTo(cx-56*s, cy-6*s).QuadTo(cx, cy+52*s, cx+56*s, cy-6*s)
				p.StrokePath(arc.Cap(ui.RoundCap), 24*s, mokoInk)
				return
			}
			var ball ui.Path
			tall := 94 * open * (1 - 0.22*sad)
			ellipse(&ball, cx, cy, 56*s, tall*s)
			p.FillPathGradient(&ball, ui.LinearGradient{Angle: 180, Stops: []ui.GradientStop{{Color: mokoInk, At: 0}, {Color: mokoIris, At: 1}}})
			if open > 0.6 {
				var glint ui.Path
				glint.Circle(cx+22*s, cy-tall*0.46*s, 20*s)
				p.FillPath(&glint, mokoWhite)
			}
		})
	}

	// Brows rise at the middle as the air gets worse: it worries, it is not cross.
	if sad > 0.05 {
		for _, side := range [2][2]float32{{-150, 1}, {86, -1}} {
			var brow ui.Path
			brow.MoveTo((side[0]-54*side[1])*s, (-122+22*sad)*s).LineTo((side[0]+40*side[1])*s, (-122-12*sad)*s)
			p.StrokePath(brow.Cap(ui.RoundCap), 22*s, mokoInk.Alpha(min(sad*2, 1)))
		}
	}

	// The mouth: a line from a frown to a smile, and laughing, open.
	mx, my := -32*s, 122*s
	smile := m.smile
	laugh := max(min((smile-0.7)/0.3, 1), 0)
	half := (34 + 10*max(smile, 0)) * s
	if laugh > 0 {
		// The stroke rounds its corners.
		var mouth ui.Path
		mouth.MoveTo(mx-half*0.82, my).QuadTo(mx, my+6*s, mx+half*0.82, my).QuadTo(mx, my+(6+84*laugh)*s, mx-half*0.82, my).Close()
		p.StrokePath(&mouth, 18*s, mokoInk)
		p.FillPath(&mouth, mokoInk)
		var tongue ui.Path
		ellipse(&tongue, mx, my+33*laugh*s, 15*laugh*s, 9*laugh*s)
		p.FillPath(&tongue, mokoBlush)
	}
	if laugh < 1 {
		var line ui.Path
		line.MoveTo(mx-half, my).QuadTo(mx, my+smile*44*s, mx+half, my)
		p.StrokePath(line.Cap(ui.RoundCap), 24*s, mokoInk.Alpha(1-laugh))
	}
}

// mascot is Moko in a square of its own, with a change of mood going smoothly. When
// kick changes, and when it is clicked, it gives a little hop.
func mascot(c *ui.Context, size float32, m mood, label string, kick any) ui.Element {
	box := ui.Box(c).Size(size, size).Role(ui.RoleImage).Label(label).Cursor(ui.CursorPointer)
	settle := ui.Animation{Duration: 700 * time.Millisecond, Ease: ui.EaseInOut}
	shown := m
	shown.smile = box.AnimateTo("smile", m.smile, settle)
	shown.eyes = box.AnimateTo("eyes", m.eyes, settle)
	shown.dust = box.AnimateTo("dust", m.dust, settle)
	shown.shade = box.AnimateColor("shade", m.shade, skyFade)
	shown.deep = box.AnimateColor("deep", m.deep, skyFade)
	drop := box.AnimateTo("drop", b2f(m.drop || m.dry), ui.Bouncy)
	hop := box.AnimateTo("hop", 1, ui.Animation{Spring: ui.SpringOptions{Duration: 520 * time.Millisecond, Bounce: 0.5}})
	last := ui.Local(box, "kick", func() any { return kick })
	if *last != kick {
		*last = kick
		box.Jump("hop", 0.95)
	}
	if box.Clicked() {
		box.Jump("hop", 0.9)
	}
	still := c.Preferences().ReduceMotion
	return box.Draw(func(p *ui.Painter, r ui.Rect) {
		t := float64(0)
		if !still {
			// A slow, soft motion: fewer frames look the same, and save power.
			p.AnimationFrameRate(30)
			t = float64(p.Now().UnixMilli()) / 1000
		}
		drawMoko(p, r, shown, t, hop, drop)
	})
}

// drawBrandWind paints the gust on the icon's tile.
func drawBrandWind(p *ui.Painter, r ui.Rect) {
	u := r.W / 1024
	x, y := r.X, r.Y
	var gust, short ui.Path
	gust.MoveTo(x-40*u, y+250*u).LineTo(x+300*u, y+250*u).CubeTo(x+390*u, y+250*u, x+420*u, y+180*u, x+380*u, y+135*u).CubeTo(x+345*u, y+95*u, x+280*u, y+110*u, x+272*u, y+165*u)
	short.MoveTo(x-40*u, y+360*u).LineTo(x+150*u, y+360*u)
	p.StrokePath(gust.Cap(ui.RoundCap), 44*u, brandWind.Alpha(0.62))
	p.StrokePath(short.Cap(ui.RoundCap), 44*u, brandWind.Alpha(0.62))
}

// brandTile is the rounded square of the icon, for Moko to lean out of.
func brandTile(c *ui.Context, size float32) ui.Element {
	return ui.Box(c).Size(size, size).Radius(size * 0.2237).Clip().
		LinearGradient(ui.LinearGradient{Angle: 135, Stops: []ui.GradientStop{{Color: brandFrom, At: 0}, {Color: brandTo, At: 1}}})
}

// brandMark is the icon of the app, drawn with the same code as the Moko of the views.
// Clip keeps what is under an element's children: its own painting clips here.
func brandMark(c *ui.Context, size float32) ui.Element {
	logo := calmMood(0.55)
	logo.drop = true
	return brandTile(c, size).Draw(func(p *ui.Painter, r ui.Rect) {
		p.Clip(r, size*0.2237, func() {
			drawBrandWind(p, r)
			drawMoko(p, r, logo, 0, 1, 1)
		})
	})
}

// mokoTile is the icon come alive, for the screens with no reading to show: Moko moves,
// and its mood is what the screen has to say.
func mokoTile(c *ui.Context, size float32, smile float32) ui.Element {
	tile := brandTile(c, size).Draw(func(p *ui.Painter, r ui.Rect) {
		p.Clip(r, size*0.2237, func() { drawBrandWind(p, r) })
	})
	return tile.Children(func() { mascot(c.Key("moko"), size, calmMood(smile), "Moko", 0) })
}

// gust is a line of wind across the sky, as fractions of it.
type gust struct {
	x, y, length, phase float32
	curl                bool
}

var gusts = [...]gust{
	{x: 0.44, y: 0.17, length: 0.16, phase: 0, curl: true},
	{x: 0.47, y: 0.33, length: 0.09, phase: 0.45},
}

// skyDecor draws on box, under its content, what the sky shows of the air and the
// device: wind, at the pace of the fan, while it runs; specks, when the air is dirty;
// and stars, while Moko sleeps.
func skyDecor(c *ui.Context, box ui.Element, m mood, fg ui.Color) {
	night := box.AnimateTo("stars", b2f(m.asleep), ui.Animation{Duration: 900 * time.Millisecond})
	dirty := box.AnimateTo("specks", m.dust, ui.Animation{Duration: 900 * time.Millisecond})
	still := c.Preferences().ReduceMotion
	breath := m.breath.Seconds()
	if breath <= 0 {
		breath = 3.6
	}
	box.Draw(func(p *ui.Painter, r ui.Rect) {
		t := float64(0)
		if !still {
			p.AnimationFrameRate(30)
			t = float64(p.Now().UnixMilli()) / 1000
		}
		if night < 0.99 {
			k := r.H * 0.11 // the size of the curl
			for _, g := range gusts {
				x, y, l := r.X+r.W*g.x, r.Y+r.H*g.y, r.W*g.length
				var path ui.Path
				path.MoveTo(x, y).LineTo(x+l, y)
				if g.curl {
					path.CubeTo(x+l+k*0.9, y, x+l+k*1.1, y-k*0.75, x+l+k*0.6, y-k*1.05).CubeTo(x+l+k*0.2, y-k*1.3, x+l-k*0.3, y-k*1.05, x+l-k*0.2, y-k*0.55)
				}
				width := max(r.H*0.017, 2.5)
				p.StrokePath(path.Cap(ui.RoundCap), width, fg.Alpha(0.2*(1-night)))
				// A gust runs along the line, at the pace of the fan. Its head moves in steps that
				// come round again, so that its masks are drawn once and reused.
				if !still {
					head := float32(math.Round(math.Mod(t/(breath*1.1)+float64(g.phase), 1)*90)) / 90 * 1.5
					p.StrokePath(path.Trim(max(head-0.5, 0), min(head, 1)), width, fg.Alpha(0.4*(1-night)))
				}
			}
		}
		if specks := max(dirty-0.2, 0) / 0.8; specks > 0.01 {
			for i, o := range [...][2]float32{{0.12, 0.72}, {0.3, 0.86}, {0.34, 0.12}, {0.5, 0.5}, {0.56, 0.08}, {0.46, 0.68}, {0.66, 0.14}, {0.26, 0.42}} {
				sway := float32(math.Sin(t*0.5+float64(i)*1.7)) * 6
				rise := float32(math.Cos(t*0.4+float64(i)*2.3)) * 5
				var speck ui.Path
				speck.Circle(r.X+r.W*o[0]+sway, r.Y+r.H*o[1]+rise, 1.8+float32(i%3)*0.9)
				p.FillPath(&speck, fg.Alpha(specks*0.45))
			}
		}
		if night > 0.01 {
			for i, o := range [...][2]float32{{0.08, 0.14}, {0.2, 0.3}, {0.32, 0.09}, {0.5, 0.16}, {0.66, 0.08}, {0.78, 0.1}, {0.92, 0.12}, {0.38, 0.5}, {0.06, 0.62}, {0.56, 0.36}} {
				twinkle := 0.55 + 0.45*float32(math.Sin(t*1.3+float64(i)*1.9))
				var star ui.Path
				star.Circle(r.X+r.W*o[0], r.Y+r.H*o[1], 1.1+float32(i%3)*0.35)
				p.FillPath(&star, mokoWhite.Alpha(night*twinkle*0.8))
			}
		}
	})
}

// homeDecor draws on box a house for Moko to lean out of, its windows lighting up and going
// out by themselves, as a home does that looks after itself.
func homeDecor(c *ui.Context, box ui.Element, s sky) {
	still := c.Preferences().ReduceMotion
	wall, roof := s.bg.Mix(s.deep, 0.2), s.bg.Mix(s.deep, 0.42)
	dark, lit := s.bg.Mix(s.deep, 0.5), ui.Oklch(0.97, 0.07, 95)
	box.Draw(func(p *ui.Painter, r ui.Rect) {
		t := float64(0)
		if !still {
			p.AnimationFrameRate(30)
			t = float64(p.Now().UnixMilli()) / 1000
		}
		right, top := r.X+r.W, r.Y
		p.Clip(r, 20, func() { // Clip on an element leaves its own Draw alone
			// A chimney behind the roof, the walls, then the roof.
			p.Fill(ui.Rect{X: right - 270, Y: top + 24, W: 26, H: 50}, roof, 5)
			p.Fill(ui.Rect{X: right - 356, Y: top + 76, W: 300, H: r.H}, wall, 8)
			var gable ui.Path
			gable.MoveTo(right-378, top+86).LineTo(right-206, top+20).LineTo(right-34, top+86).Close()
			p.FillPath(&gable, roof)
			p.StrokePath(gable.Join(ui.RoundJoin), 12, roof)
			// Four panes, each with its own slow turn: at least two are always on.
			const pane, gap = 25, 4
			x0, y0 := right-330, top+104
			for i, phase := range [...]float64{0, 2.1, 4.4, 1.2} {
				on := float32(1)
				if !still && i < 2 {
					on = min(max(float32(math.Sin(t*0.45+phase))*2+0.5, 0), 1)
				}
				cell := ui.Rect{X: x0 + float32(i%2)*(pane+gap), Y: y0 + float32(i/2)*(pane+gap), W: pane, H: pane}
				p.Fill(cell, dark, 5)
				p.Fill(cell, lit.Alpha(on), 5)
			}
		})
	})
}

// signalDecor draws on box the device's signal: rings around Moko, with a wave going out
// through them while it is answering or being looked for. Without pulse they are still.
func signalDecor(c *ui.Context, box ui.Element, fg ui.Color, pulse bool) {
	still := c.Preferences().ReduceMotion || !pulse
	box.Draw(func(p *ui.Painter, r ui.Rect) {
		t := float64(0)
		if !still {
			p.AnimationFrameRate(30)
			t = float64(p.Now().UnixMilli()) / 1000
		}
		cx, cy := r.X+r.W-40, r.Y+r.H-34
		const rings = 5
		wave := float32(math.Mod(t/3, 1)) * (rings + 2)
		p.Clip(r, 0, func() { // Clip on an element leaves its own Draw alone
			for i := range rings {
				alpha := float32(0.13)
				if !still {
					near := 1 - min(float32(math.Abs(float64(float32(i)-wave+1)))/1.2, 1)
					alpha += 0.32 * near
				}
				var ring ui.Path
				ring.Circle(cx, cy, 64+float32(i)*22)
				p.StrokePath(&ring, 2.5, fg.Alpha(alpha))
			}
		})
	})
}

func b2f(b bool) float32 {
	if b {
		return 1
	}
	return 0
}
