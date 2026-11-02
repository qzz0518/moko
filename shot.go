//go:build shot && !windows

package main

import (
	"log"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/egoist/mygo"

	"github.com/qzz0518/moko/internal/i18n"
	"github.com/qzz0518/moko/internal/view"
)

func cpuOver(d time.Duration) float64 {
	used := func() time.Duration {
		var r syscall.Rusage
		_ = syscall.Getrusage(syscall.RUSAGE_SELF, &r)
		return time.Duration(r.Utime.Nano() + r.Stime.Nano())
	}
	start := used()
	time.Sleep(d)
	return float64(used()-start) / float64(d) * 100
}

// With the shot tag and MOKO_SHOT set to a directory, the app photographs its real
// windows as they run on this machine and quits. It only looks: nothing is clicked.
func init() {
	dir := os.Getenv("MOKO_SHOT")
	if dir == "" {
		return
	}
	afterReady = func(a *app) {
		go func() {
			save := func(name string, w *mygo.Window) {
				b, err := w.CapturePage()
				if err != nil {
					log.Printf("capture %s: %v", name, err)
					return
				}
				if err := os.WriteFile(filepath.Join(dir, name+".png"), b, 0o644); err != nil {
					log.Print(err)
				}
			}
			time.Sleep(6 * time.Second)
			for _, p := range []struct {
				name string
				page view.Page
			}{{"real-overview", view.PageOverview}, {"real-home", view.PageHome}, {"real-settings", view.PageSettings}} {
				mygo.RunOnMain(func() { a.v.Page = p.page; a.win.Invalidate() })
				time.Sleep(800 * time.Millisecond)
				save(p.name, a.win)
			}
			mygo.RunOnMain(a.toggleFlyout)
			time.Sleep(1500 * time.Millisecond)
			if a.fly != nil {
				save("real-flyout", a.fly)
			}
			mygo.RunOnMain(a.toggleFlyout)
			// What the animations cost: the window open, and in the tray alone, which is
			// where the app spends its days.
			log.Printf("locale %q, language %q", mygo.App.Locale(), i18n.Lang())
			mygo.RunOnMain(func() { a.v.Page = view.PageOverview; a.win.Invalidate() })
			time.Sleep(time.Second)
			log.Printf("window open: %.1f%% of a core", cpuOver(10*time.Second))
			mygo.RunOnMain(a.win.Hide)
			time.Sleep(time.Second)
			log.Printf("window hidden: %.1f%% of a core", cpuOver(10*time.Second))
			mygo.App.Quit()
		}()
	}
}
