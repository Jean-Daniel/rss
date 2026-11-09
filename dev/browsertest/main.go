// Command browsertest drives a headless Chromium against the app to check
// what unit tests cannot: the service worker, installability, the install
// button and the phone gestures. run.sh sets up the containers it expects.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/cdproto/input"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/remote"
)

// The app as Chromium sees it (forwarded to nginx inside its container).
const base = "http://localhost:8080"

var (
	devtools = flag.String("devtools", "ws://127.0.0.1:9222/", "Chromium DevTools address")
	password = flag.String("password", "", "file holding the login password")
	current  = flag.String("current", "", "folder the app container runs its binary from")
	next     = flag.String("next", "", "folder holding the next version's binary")
	app      = flag.String("app", "", "app container")
	proxy    = flag.String("proxy", "", "nginx container")
)

// JavaScript snippets.
const (
	cacheState = `(async () => {
		const out = {};
		for (const key of await caches.keys()) out[key] = (await (await caches.open(key)).keys()).length;
		return out;
	})()`
	appVersion   = `Object.entries(window).find(([k]) => k.startsWith('__sveltekit_'))?.[1]?.version ?? ''`
	readerShown  = `!!document.querySelector('.sidebar .logo')`
	unreachable  = `document.body.innerText.includes('The server cannot be reached.')`
	drawerOpen   = `document.querySelector('.shell').classList.contains('drawer-open')`
	installMatch = `[...document.querySelectorAll('button')].find(b => b.textContent.includes('Install app'))`
)

var failures int

func check(ok bool, format string, args ...any) {
	status := "ok  "
	if !ok {
		status = "FAIL"
		failures++
	}
	fmt.Printf("%s %s\n", status, fmt.Sprintf(format, args...))
}

func must(err error) {
	if err != nil {
		fmt.Println("error:", err)
		os.Exit(1)
	}
}

func podman(args ...string) {
	out, err := exec.Command("podman", args...).CombinedOutput()
	if err != nil {
		fmt.Printf("podman %s: %v\n%s", strings.Join(args, " "), err, out)
		os.Exit(1)
	}
}

func eval[T any](ctx context.Context, js string) T {
	out, err := chromedp.Run(ctx, chromedp.Evaluate[T](js, chromedp.EvalAwaitPromise))
	must(err)
	return out
}

// waitFor polls a JavaScript condition until it is true.
func waitFor(ctx context.Context, js string, timeout time.Duration) bool {
	for deadline := time.Now().Add(timeout); time.Now().Before(deadline); time.Sleep(200 * time.Millisecond) {
		ok, err := chromedp.Run(ctx, chromedp.Evaluate[bool](js, chromedp.EvalAwaitPromise))
		if err == nil && ok {
			return true
		}
	}
	return false
}

func navigate(ctx context.Context, url string) {
	must(chromedp.Do(ctx, chromedp.Navigate(url)))
}

// openReader loads the reader, retrying while the app starts.
func openReader(ctx context.Context) bool {
	for range 10 {
		navigate(ctx, base+"/")
		if waitFor(ctx, readerShown, 3*time.Second) {
			return true
		}
	}
	return false
}

func touch(ctx context.Context, typ input.DispatchTouchEventType, x, y float64) {
	points := []*input.TouchPoint{}
	if typ != input.DispatchTouchEventTypeTouchEnd {
		points = append(points, &input.TouchPoint{X: x, Y: y})
	}
	_, err := chromedp.Call(ctx, input.DispatchTouchEvent, input.DispatchTouchEventParams{Type: typ, TouchPoints: points})
	must(err)
}

func tap(ctx context.Context, x, y float64) {
	touch(ctx, input.DispatchTouchEventTypeTouchStart, x, y)
	touch(ctx, input.DispatchTouchEventTypeTouchEnd, 0, 0)
}

func swipe(ctx context.Context, x0, y0, x1, y1 float64, steps int, stepDelay time.Duration) {
	touch(ctx, input.DispatchTouchEventTypeTouchStart, x0, y0)
	for i := 1; i <= steps; i++ {
		f := float64(i) / float64(steps)
		touch(ctx, input.DispatchTouchEventTypeTouchMove, x0+(x1-x0)*f, y0+(y1-y0)*f)
		time.Sleep(stepDelay)
	}
	touch(ctx, input.DispatchTouchEventTypeTouchEnd, 0, 0)
	time.Sleep(400 * time.Millisecond) // the drawer's transition
}

func main() {
	flag.Parse()
	secret, err := os.ReadFile(*password)
	must(err)

	alloc, cancel := remote.NewAllocator(context.Background(), *devtools)
	defer cancel()
	ctx, cancel := chromedp.NewContext(alloc)
	defer cancel()
	ctx, cancel = context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	firstVisit(ctx)
	signIn(ctx, strings.TrimSpace(string(secret)))
	serverProblems(ctx)
	update(ctx)
	phone(ctx)
	installButton(ctx)

	if failures > 0 {
		fmt.Printf("%d check(s) failed\n", failures)
		os.Exit(1)
	}
	fmt.Println("all checks passed")
}

// The service worker installs and keeps the app's files; Chrome finds the
// app installable.
func firstVisit(ctx context.Context) {
	navigate(ctx, base+"/")
	check(waitFor(ctx, `location.pathname === '/login'`, 10*time.Second), "signed out: redirected to /login")
	check(waitFor(ctx, `navigator.serviceWorker.controller !== null`, 15*time.Second), "service worker controls the page")
	caches := eval[map[string]int](ctx, cacheState)
	check(len(caches) == 1, "one cache after install: %v", caches)
	for _, n := range caches {
		check(n > 10, "precached %d files", n)
	}
	check(eval[bool](ctx, `caches.match('/').then(r => !!r)`), "app shell cached")

	inst, err := chromedp.Call(ctx, page.GetInstallabilityErrors, cdp.Empty{})
	must(err)
	var errs []string
	for _, e := range inst.InstallabilityErrors {
		errs = append(errs, e.ErrorID)
	}
	check(len(errs) == 0, "installable (errors: %v)", errs)
	manifest, err := chromedp.Call(ctx, page.GetAppManifest, page.GetAppManifestParams{})
	must(err)
	var merrs []string
	for _, e := range manifest.Errors {
		merrs = append(merrs, e.Message)
	}
	check(len(merrs) == 0, "manifest parses cleanly (errors: %v)", merrs)
}

func signIn(ctx context.Context, secret string) {
	status := eval[int](ctx, fmt.Sprintf(`fetch('/api/session', {
		method: 'POST', headers: {'Content-Type': 'application/json'}, body: JSON.stringify({password: %q})
	}).then(r => r.status)`, secret))
	check(status == 200, "signed in (%d)", status)
	check(openReader(ctx), "reader shown")
}

// The app still opens when the server is down (nginx answers 502), when
// there is no network at all, and when the server does not answer.
func serverProblems(ctx context.Context) {
	podman("stop", "-t", "0", *app)
	navigate(ctx, base+"/saved")
	check(waitFor(ctx, unreachable, 10*time.Second), "server down behind nginx: the app opens and says the server cannot be reached")

	podman("stop", "-t", "0", *proxy)
	navigate(ctx, base+"/")
	check(waitFor(ctx, unreachable, 10*time.Second), "no network: the app opens and says the server cannot be reached")

	podman("start", *proxy)
	podman("start", *app)
	check(openReader(ctx), "server back: reader shown")

	podman("pause", *app)
	start := time.Now()
	navigate(ctx, base+"/")
	loaded := time.Since(start)
	check(loaded < 6*time.Second, "server not answering: the page loads from the cache after the timeout (%v)", loaded.Round(100*time.Millisecond))
	check(waitFor(ctx, unreachable, 20*time.Second), "server not answering: the app says it cannot be reached (after %v)", time.Since(start).Round(100*time.Millisecond))
	podman("unpause", *app)
	check(openReader(ctx), "server answering again: reader shown")
}

// A new version is deployed while the page stays open.
func update(ctx context.Context) {
	oldVersion := eval[string](ctx, appVersion)
	oldCaches := eval[map[string]int](ctx, cacheState)

	podman("stop", "-t", "0", *app)
	bin, err := os.ReadFile(*next + "/fever")
	must(err)
	must(os.WriteFile(*current+"/fever", bin, 0o755))
	podman("start", *app)
	waitFor(ctx, `fetch('/api/session').then(r => r.ok)`, 10*time.Second)

	check(!eval[bool](ctx, `!!document.querySelector('.update')`), "no update banner before the check")
	// SvelteKit checks for a new version when the window regains focus.
	eval[bool](ctx, `window.dispatchEvent(new Event('focus')), true`)
	check(waitFor(ctx, `document.querySelector('.update')?.innerText.includes('A new version of Fever is available.') ?? false`, 10*time.Second),
		"update banner shown")
	eval[bool](ctx, `document.querySelector('.update button').click(), true`)
	time.Sleep(time.Second)
	check(waitFor(ctx, readerShown, 10*time.Second), "Reload: reader shown")
	newVersion := eval[string](ctx, appVersion)
	check(newVersion != "" && newVersion != oldVersion, "new version running (%s → %s)", oldVersion, newVersion)
	check(!eval[bool](ctx, `!!document.querySelector('.update')`), "no banner after the reload")
	check(waitFor(ctx, `caches.keys().then(k => k.length === 2)`, 15*time.Second), "new service worker installed")
	caches := eval[map[string]int](ctx, cacheState)
	kept := true
	for key := range oldCaches {
		_, ok := caches[key]
		kept = kept && ok
	}
	check(kept, "previous version's files kept: %v", caches)

	// Offline, the new version starts, not the previous one.
	podman("stop", "-t", "0", *app)
	navigate(ctx, base+"/")
	check(waitFor(ctx, unreachable, 10*time.Second), "server down after the update: the app opens")
	offlineVersion := eval[string](ctx, appVersion)
	check(offlineVersion == newVersion, "offline start uses the new version (%s)", offlineVersion)
	podman("start", *app)
	check(openReader(ctx), "server back: reader shown")
}

// The phone layout: drawer swipes, title tap.
func phone(ctx context.Context) {
	_, err := chromedp.Call(ctx, emulation.SetDeviceMetricsOverride, emulation.SetDeviceMetricsOverrideParams{Width: 390, Height: 844, DeviceScaleFactor: 3, Mobile: true})
	must(err)
	_, err = chromedp.Call(ctx, emulation.SetTouchEmulationEnabled, emulation.SetTouchEmulationEnabledParams{Enabled: true, MaxTouchPoints: 5})
	must(err)
	navigate(ctx, base+"/")
	check(waitFor(ctx, `getComputedStyle(document.querySelector('.topbar')).display === 'flex'`, 10*time.Second), "phone layout: top bar shown")

	openDrawer := func(when string) {
		time.Sleep(300 * time.Millisecond)
		tap(ctx, 24, 24)
		check(waitFor(ctx, drawerOpen, time.Second), "menu button opens the drawer (%s)", when)
		time.Sleep(300 * time.Millisecond)
	}
	openDrawer("first")
	swipe(ctx, 250, 400, 215, 400, 6, 60*time.Millisecond)
	check(eval[bool](ctx, drawerOpen), "short slow swipe: drawer stays open")
	check(eval[string](ctx, `document.querySelector('.panels').style.transform`) == "", "drawer snaps back")
	swipe(ctx, 200, 300, 205, 600, 8, 20*time.Millisecond)
	check(eval[bool](ctx, drawerOpen), "vertical move: drawer stays open")
	swipe(ctx, 250, 400, 60, 410, 10, 20*time.Millisecond)
	check(!eval[bool](ctx, drawerOpen), "long left swipe closes the drawer")
	check(eval[bool](ctx, `document.querySelector('.panels').inert`), "closed drawer is inert")
	openDrawer("before a flick")
	swipe(ctx, 250, 400, 190, 400, 3, 10*time.Millisecond)
	check(!eval[bool](ctx, drawerOpen), "quick flick closes the drawer")
	openDrawer("before a right swipe")
	swipe(ctx, 150, 400, 300, 400, 8, 20*time.Millisecond)
	check(eval[bool](ctx, drawerOpen), "right swipe: drawer stays open")
	tap(ctx, 380, 400)
	time.Sleep(400 * time.Millisecond)
	check(!eval[bool](ctx, drawerOpen), "backdrop tap closes the drawer")

	// Tall enough to scroll, whatever is left unread in the sample data.
	eval[bool](ctx, `document.querySelector('.shell').style.minHeight = '3000px', window.scrollTo(0, 600), true`)
	time.Sleep(300 * time.Millisecond)
	check(eval[float64](ctx, `scrollY`) > 100, "scrolled down")
	tap(ctx, 195, 24)
	check(waitFor(ctx, `scrollY === 0`, 3*time.Second), "title tap scrolls back to the top")
}

// Headless Chromium never offers installation, so the browser's offer is faked.
func installButton(ctx context.Context) {
	check(!eval[bool](ctx, `!!`+installMatch), "no Install app button without the browser's offer")
	eval[bool](ctx, `(() => {
		const e = new Event('beforeinstallprompt', { cancelable: true });
		e.prompt = () => { window.__prompted = true; return Promise.resolve(); };
		e.userChoice = Promise.resolve({ outcome: 'accepted' });
		window.dispatchEvent(e);
		window.__defaultPrevented = e.defaultPrevented;
		return true;
	})()`)
	check(waitFor(ctx, `!!`+installMatch, 2*time.Second), "browser offers installation: Install app button shown")
	check(eval[bool](ctx, `window.__defaultPrevented`), "browser's own install banner suppressed")
	eval[bool](ctx, installMatch+`.click(), true`)
	check(waitFor(ctx, `window.__prompted === true`, 2*time.Second), "button opens the browser's install dialog")
	check(waitFor(ctx, `!`+installMatch, 2*time.Second), "button hidden afterwards")
}
