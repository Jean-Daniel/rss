// Command browsertest drives a headless Chromium against the app to check
// what unit tests cannot: the service worker, installability, the install
// button, the phone gestures, reading while the server has problems, and
// managing feeds, searching and the keyboard shortcuts. run.sh sets up the
// containers it expects.
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
	"github.com/chromedp/chromedp/kb"
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
	itemsShown   = readerShown + ` && document.querySelectorAll('.item').length > 0`
	// A promise of the menu item of the list's menu (opened first, then drawn)
	// whose text starts so, or null.
	menuItem = `(async (label) => {
		const button = [...document.querySelectorAll('.list-menu > button')].find(b => b.offsetParent);
		if (button && button.getAttribute('aria-expanded') !== 'true') {
			button.click();
			await new Promise((done) => setTimeout(done, 50));
		}
		return [...document.querySelectorAll('.list-menu [role="menuitem"]')].find(i => i.textContent.trim().startsWith(label)) ?? null;
	})`
	firstSave   = `document.querySelector('.item .action.save')`
	firstToggle = `document.querySelector('.item .action.read-toggle')`
	savedCount  = `(document.querySelector('a[href="/saved"] .count')?.textContent.trim() ?? '0')`
	allSent     = `localStorage.getItem('fever.pendingReads') === null`
	noAlerts    = `window.__test.alerts.length === 0`

	// Runs before the app's own scripts, in every page loaded after it is
	// added. It records the API calls, and can make /api/subscriptions fail
	// or report a refresh in progress, or delay the next pages of a list. The
	// bodies sent are recorded too. An alert or a confirmation would
	// block the page: alerts are recorded, confirmations accepted. Tabs
	// opened (window.open) are recorded, not opened.
	instrument = `(() => {
		const real = window.fetch;
		const t = (window.__test = { calls: [], bodies: [], alerts: [], opened: [], fail: false, refreshing: false, slowPages: false });
		window.fetch = async (input, init) => {
			const request = input instanceof Request ? input : null;
			const url = new URL(request ? request.url : String(input), location.href);
			if (url.pathname.startsWith('/api/')) {
				t.calls.push((init?.method ?? request?.method ?? 'GET') + ' ' + url.pathname);
				if (typeof init?.body === 'string') t.bodies.push(url.pathname + ' ' + init.body);
			}
			// The pages after the first, slow to come.
			if (t.slowPages && url.pathname === '/api/items' && url.searchParams.has('after')) {
				await new Promise((done) => setTimeout(done, 1500));
			}
			if (url.pathname !== '/api/subscriptions') return real(input, init);
			if (t.fail) return new Response('down', { status: 503 });
			const res = await real(input, init);
			if (!t.refreshing || !res.ok) return res;
			return new Response(JSON.stringify({ ...(await res.json()), refreshing: true }), {
				status: res.status,
				headers: { 'Content-Type': 'application/json' }
			});
		};
		window.alert = (message) => void t.alerts.push(String(message));
		window.open = (url) => (t.opened.push(String(url)), null);
		window.confirm = () => true;
	})()`
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
	ctx, cancel = context.WithTimeout(ctx, 8*time.Minute)
	defer cancel()

	firstVisit(ctx)
	signIn(ctx, strings.TrimSpace(string(secret)))
	serverProblems(ctx)
	update(ctx)
	phone(ctx)
	installButton(ctx)
	reading(ctx, strings.TrimSpace(string(secret)))
	manage(ctx)
	keyboard(ctx)

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

// Reading while the server has problems: a counts reload that fails, a
// refresh in progress, "mark all as read" right after a change, and a
// session that has ended.
func reading(ctx context.Context, secret string) {
	_, err := chromedp.Call(ctx, emulation.ClearDeviceMetricsOverride, cdp.Empty{})
	must(err)
	_, err = chromedp.Call(ctx, emulation.SetTouchEmulationEnabled, emulation.SetTouchEmulationEnabledParams{Enabled: false})
	must(err)
	_, err = chromedp.Call(ctx, page.AddScriptToEvaluateOnNewDocument, page.AddScriptToEvaluateOnNewDocumentParams{Source: instrument})
	must(err)

	do := func(js string) { eval[bool](ctx, js+`, true`) }
	forgetCalls := func() { do(`window.__test.calls.length = 0`) }
	calls := func(call string) int {
		return eval[int](ctx, fmt.Sprintf(`window.__test.calls.filter(c => c === %q).length`, call))
	}
	attr := func(element, name string) string {
		return eval[string](ctx, fmt.Sprintf(`%s.getAttribute(%q)`, element, name))
	}

	// Lists that include read items, so the checks do not depend on what the
	// steps before this one left unread.
	do(`localStorage.setItem('fever.showRead', '1')`)
	navigate(ctx, base+"/")
	check(waitFor(ctx, itemsShown, 10*time.Second), "desktop layout: reader shown, with items")

	// Saving an item reloads the counts. That reload fails.
	before := eval[string](ctx, savedCount)
	saved := attr(firstSave, "aria-pressed")
	do(`window.__test.fail = true`)
	forgetCalls()
	do(firstSave + `.click()`)
	check(waitFor(ctx, `window.__test.calls.includes('GET /api/subscriptions')`, 5*time.Second), "saving an item reloads the counts")
	time.Sleep(500 * time.Millisecond) // time for an error page to replace the reader
	check(eval[bool](ctx, itemsShown), "counts reload fails: the reader stays on screen")
	check(attr(firstSave, "aria-pressed") != saved, "counts reload fails: the item stays saved")
	check(eval[bool](ctx, noAlerts), "counts reload fails: no error message")
	do(`window.__test.fail = false`)
	check(waitFor(ctx, fmt.Sprintf(`%s !== %q`, savedCount, before), 25*time.Second), "server answers again: the counts are reloaded without being asked")

	// A refresh is running on the server, as far as the page can tell: the
	// counts are reloaded every 2 seconds, also while items are being read.
	do(`window.__test.refreshing = true`)
	do(firstSave + `.click()`)
	check(waitFor(ctx, fmt.Sprintf(`%s === %q`, savedCount, before), 5*time.Second), "item no longer saved, counts reloaded")
	forgetCalls()
	start := time.Now()
	for range 30 { // an even number: the item ends as it started
		do(firstToggle + `.click()`)
		time.Sleep(300 * time.Millisecond)
	}
	reloads := calls("GET /api/subscriptions")
	check(reloads >= 3, "refresh in progress: the counts keep being reloaded while items are read (%d reloads in %v)",
		reloads, time.Since(start).Round(time.Second))
	do(`window.__test.refreshing = false`)
	time.Sleep(3 * time.Second) // the next reload says the refresh is over
	forgetCalls()
	time.Sleep(5 * time.Second)
	reloads = calls("GET /api/subscriptions")
	check(reloads <= 1, "refresh over: the reloading stops (%d reloads in 5 s)", reloads)

	// "Mark all as read" right after an item was marked unread: the change
	// must reach the server first, or it would undo part of it afterwards.
	if attr(firstToggle, "aria-pressed") == "true" { // unread
		do(firstToggle + `.click()`)
	}
	check(waitFor(ctx, allSent, 6*time.Second), "earlier changes sent")
	forgetCalls()
	enabled := eval[bool](ctx, `(async () => {
		`+firstToggle+`.click();
		await new Promise(done => setTimeout(done, 100));
		const all = await `+menuItem+`('Mark all as read');
		all?.click();
		return !!all;
	})()`)
	check(enabled, "item marked unread: Mark all as read is in the list's menu")
	check(waitFor(ctx, `window.__test.calls.includes('POST /api/items/read-all')`, 5*time.Second), "Mark all as read: sent")
	order := eval[[]string](ctx, `window.__test.calls.filter(c => c.startsWith('POST /api/items/'))`)
	check(len(order) >= 2 && order[0] == "POST /api/items/state" && order[1] == "POST /api/items/read-all",
		"Mark all as read: the change made just before goes first (%v)", order)
	time.Sleep(3 * time.Second) // a change held back would have been sent by now
	unread := eval[int](ctx, `fetch('/api/subscriptions').then(r => r.json()).then(s => s.totalUnread)`)
	check(unread == 0, "Mark all as read: nothing left unread on the server (%d)", unread)
	check(eval[bool](ctx, allSent), "Mark all as read: nothing left to send")
	check(waitFor(ctx, `!document.querySelector('a[href="/"] .count') && `+firstToggle+`.getAttribute('aria-pressed') === 'false'`, 3*time.Second),
		"Mark all as read: no unread count and no unread item on screen")
	check(eval[bool](ctx, noAlerts), "Mark all as read: no error message")

	// The session ends while a list is open: the feed of the first item,
	// which has items whatever the sample data's dates have let expire.
	list := eval[string](ctx, `document.querySelector('.item a.feed')?.getAttribute('href') ?? ''`)
	check(strings.HasPrefix(list, "/feeds/"), "first item links to its feed (%q)", list)
	navigate(ctx, base+list)
	check(waitFor(ctx, itemsShown, 10*time.Second), "%s shown, with items", list)
	status := eval[int](ctx, `fetch('/api/session', { method: 'DELETE' }).then(r => r.status)`)
	check(status == 204, "session ended behind the page's back (%d)", status)
	forgetCalls()
	do(firstToggle + `.click()`)
	check(waitFor(ctx, fmt.Sprintf(`location.pathname === '/login' && new URLSearchParams(location.search).get('next') === %q`, list), 6*time.Second),
		"session ended: sending a change opens the login page, to come back to %s", list)
	time.Sleep(7 * time.Second) // a second attempt would have been made by now
	attempts := calls("POST /api/items/state")
	check(attempts == 1, "session ended: the change is not sent again before logging in (%d attempts)", attempts)
	check(!eval[bool](ctx, allSent), "session ended: the change is kept")
	do(fmt.Sprintf(`(() => {
		const field = document.querySelector('#password');
		field.value = %q;
		field.dispatchEvent(new Event('input', { bubbles: true }));
		field.form.requestSubmit();
	})()`, secret))
	check(waitFor(ctx, fmt.Sprintf(`location.pathname === %q && %s`, list, itemsShown), 10*time.Second), "logged in again: back on %s", list)
	check(waitFor(ctx, allSent, 6*time.Second), "logged in again: the change kept meanwhile is sent")
	check(eval[bool](ctx, noAlerts), "session ended: no error message")
}

// Adding a feed from its site's address, renaming it and unsubscribing, as
// the reader would. The site is served by the nginx container.
func manage(ctx context.Context) {
	do := func(js string) { eval[bool](ctx, js+`, true`) }
	fill := func(selector, value string) {
		do(fmt.Sprintf(`(() => {
			const field = document.querySelector(%q);
			field.value = %q;
			field.dispatchEvent(new Event('input', { bubbles: true }));
		})()`, selector, value))
	}
	submit := func(selector string) { do(fmt.Sprintf(`document.querySelector(%q).form.requestSubmit()`, selector)) }
	const heading = `document.querySelector('.content-header h1')?.textContent`

	navigate(ctx, base+"/")
	check(waitFor(ctx, readerShown, 10*time.Second), "reader shown")
	do(`document.querySelector('a[href="/settings"]').click()`)
	check(waitFor(ctx, `!!document.querySelector('a[href="/feeds/new"]')`, 5*time.Second), "settings page shown")
	do(`document.querySelector('a[href="/feeds/new"]').click()`)
	const address = `input[placeholder="https://example.com"]`
	check(waitFor(ctx, fmt.Sprintf(`!!document.querySelector(%q)`, address), 5*time.Second), "Add a feed: form shown")
	fill(address, "http://"+*proxy+":8081/")
	submit(address)
	check(waitFor(ctx, `/^\/feeds\/\d+$/.test(location.pathname) && document.querySelectorAll('.item').length === 2`, 15*time.Second),
		"Add a feed: the site's feed is found and added, its items shown")
	feed := eval[string](ctx, `location.pathname`)
	check(eval[string](ctx, heading) == "Browser test feed", "Add a feed: titled after the feed (%q)", eval[string](ctx, heading))
	check(eval[bool](ctx, `[...document.querySelectorAll('.feeds a')].some(a => a.getAttribute('href') === '`+feed+`')`),
		"Add a feed: listed with the other feeds")

	eval[bool](ctx, `(async () => ((await `+menuItem+`('Edit this feed')).click(), true))()`)
	const name = `input[placeholder="The feed's own title"]`
	check(waitFor(ctx, fmt.Sprintf(`!!document.querySelector(%q)`, name), 5*time.Second), "Edit: feed settings shown")
	fill(name, "Renamed feed")
	submit(name)
	check(waitFor(ctx, fmt.Sprintf(`location.pathname === %q && %s === 'Renamed feed'`, feed, heading), 5*time.Second),
		"Edit: renamed, back on the feed")

	search(ctx)

	navigate(ctx, base+feed+"/edit")
	check(waitFor(ctx, `!!document.querySelector('button.danger')`, 10*time.Second), "Edit: page opened directly")
	do(`document.querySelector('button.danger').click()`) // confirmed by the instrument script
	check(waitFor(ctx, `location.pathname === '/'`, 5*time.Second), "Unsubscribe: back to all items")
	gone := eval[bool](ctx, `fetch('/api/subscriptions').then(r => r.json()).then(s => !s.feeds.some(f => '/feeds/' + f.id === '`+feed+`'))`)
	check(gone, "Unsubscribe: the feed is gone")
	check(eval[bool](ctx, `!document.querySelector('.feeds a[href="`+feed+`"]')`), "Unsubscribe: not listed any more")
	check(eval[bool](ctx, noAlerts), "managing feeds: no error message")
}

// Searching, in the items of the feed manage added.
func search(ctx context.Context) {
	const field = `input[name="q"]`
	query := func(words string) {
		eval[bool](ctx, fmt.Sprintf(`(() => {
			const field = document.querySelector(%q);
			field.value = %q;
			field.form.requestSubmit();
			return true;
		})()`, field, words))
	}
	titles := `[...document.querySelectorAll('.item .title')].map(t => t.textContent.trim())`

	eval[bool](ctx, `document.querySelector('a[href="/search"]').click(), true`)
	check(waitFor(ctx, fmt.Sprintf(`location.pathname === '/search' && document.activeElement === document.querySelector(%q)`, field), 5*time.Second),
		"Search: page shown, ready to type in")
	query("SECOND tes")
	check(waitFor(ctx, `new URLSearchParams(location.search).get('q') === 'SECOND tes' && document.querySelectorAll('.item').length > 0`, 10*time.Second),
		"Search: results shown")
	found := eval[[]string](ctx, titles)
	check(len(found) == 1 && found[0] == "Second test post", "Search: every word, case and word starts ignored (%q)", found)
	check(eval[bool](ctx, `(async () => { const m = `+menuItem+`; return !(await m('Show read')) && !(await m('Hide read')) && !(await m('Mark all')); })()`),
		"Search: no Unread/All or Mark all as read in the menu")
	must(chromedp.Do(ctx, chromedp.KeyEvent(kb.Escape)))
	check(waitFor(ctx, `!document.querySelector('.list-menu [role="menu"]')`, 2*time.Second), "Esc: the menu closes")
	query("nothing-like-this")
	check(waitFor(ctx, `document.querySelector('.empty')?.textContent.includes('Nothing found for')`, 10*time.Second),
		"Search: nothing found, said so")
	eval[bool](ctx, `(history.back(), true)`)
	check(waitFor(ctx, `new URLSearchParams(location.search).get('q') === 'SECOND tes' && document.querySelectorAll('.item').length === 1`, 10*time.Second),
		"Search: Back shows the previous results")

	// The same words again: searched again, not ignored.
	eval[bool](ctx, `(window.__test.calls.length = 0, true)`)
	query("SECOND tes")
	check(waitFor(ctx, `window.__test.calls.some(c => c === 'GET /api/items')`, 5*time.Second),
		"Search: the same words again search again")
}

// The keyboard shortcuts, on a feed of 25 long items served by nginx (more
// than a page).
func keyboard(ctx context.Context) {
	press := func(keys string, mods ...chromedp.Modifier) {
		must(chromedp.Do(ctx, chromedp.KeyEvent(keys, chromedp.KeyModifiers(mods...))))
		time.Sleep(150 * time.Millisecond)
	}
	shift := chromedp.ModifierShift
	at := func(path string) string { return fmt.Sprintf(`location.pathname === %q`, path) }
	const (
		current = `(document.querySelector('.item.current .title')?.textContent.trim() ?? '')`
		dialog  = `!!document.querySelector('dialog[open]')`
	)
	read := func(n int) string {
		return fmt.Sprintf(`document.querySelectorAll('.item')[%d].classList.contains('read')`, n-1)
	}
	opened := func(url string) string { return fmt.Sprintf(`window.__test.opened.includes(%q)`, url) }
	site := "http://" + *proxy + ":8081/"

	id := eval[int](ctx, fmt.Sprintf(`fetch('/api/feeds', {
		method: 'POST', headers: { 'Content-Type': 'application/json' },
		body: JSON.stringify({ url: %q, groupIds: [] })
	}).then(r => r.json()).then(r => r.feedId)`, site+"keys.xml"))
	feed := fmt.Sprintf("/feeds/%d", id)
	navigate(ctx, base+feed)
	check(waitFor(ctx, `document.querySelectorAll('.item').length === 20`, 10*time.Second), "Keys: a page of 20 items")

	press("j")
	check(waitFor(ctx, current+` === 'Key item 2'`, 3*time.Second), "J: to the next item, marked as the current one (%q)", eval[string](ctx, current))
	check(waitFor(ctx, read(1), 3*time.Second), "J: the item scrolled past is read")
	press("k")
	check(waitFor(ctx, current+` === 'Key item 1'`, 3*time.Second), "K: back to the previous item")
	press("jj")
	check(waitFor(ctx, current+` === 'Key item 3'`, 3*time.Second), "J J: two items on")
	press("s")
	check(waitFor(ctx, `document.querySelectorAll('.item')[2].querySelector('.action.save').getAttribute('aria-pressed') === 'true'`, 3*time.Second),
		"S: the current item is saved")
	press("o")
	check(waitFor(ctx, opened(site+"k3"), 2*time.Second) && waitFor(ctx, read(3), 2*time.Second), "O: the current item opens in a new tab, marked read")
	press("v")
	check(waitFor(ctx, opened(site), 2*time.Second), "V: the current item's site opens in a new tab")
	press("z")
	check(waitFor(ctx, `!(`+read(1)+` || `+read(2)+` || `+read(3)+`)`, 5*time.Second), "Z: the items just read are unread again")

	// A click in an item lower on the screen makes it the current one, and
	// the keys go on from there.
	at5 := eval[[]float64](ctx, `(() => {
		const item = document.querySelectorAll('.items > [data-id]')[4];
		window.scrollTo(0, scrollY + item.getBoundingClientRect().top - innerHeight * 0.6);
		const text = item.querySelector('.content').getBoundingClientRect();
		return [text.left + 30, text.top + 10];
	})()`)
	check(waitFor(ctx, current+` === 'Key item 4'`, 2*time.Second), "item 4 at the top, item 5 below it (%q)", eval[string](ctx, current))
	must(chromedp.Do(ctx, chromedp.MouseClickXY(at5[0], at5[1])))
	check(waitFor(ctx, current+` === 'Key item 5'`, 2*time.Second), "click: the item clicked is the current one (%q)", eval[string](ctx, current))
	press("j")
	check(waitFor(ctx, current+` === 'Key item 6'`, 3*time.Second), "J after a click: the item after the one clicked (%q)", eval[string](ctx, current))
	press("k")
	check(waitFor(ctx, current+` === 'Key item 5'`, 3*time.Second), "K: back to the one clicked (%q)", eval[string](ctx, current))
	check(eval[bool](ctx, `document.activeElement === document.querySelectorAll('.item')[4]`), "K: the keyboard focus is on it too")

	// Scrolling away on one's own lets the chosen item go.
	eval[bool](ctx, `(window.scrollBy(0, -innerHeight / 2), true)`)
	check(waitFor(ctx, current+` === 'Key item 4'`, 2*time.Second), "scrolled up half a screen: the item at the top is current again (%q)", eval[string](ctx, current))
	press(kb.Escape)
	check(waitFor(ctx, `!document.querySelector('.item.current')`, 2*time.Second), "Esc: nothing outlined")
	_, err := chromedp.Call(ctx, input.DispatchMouseEvent, input.DispatchMouseEventParams{Type: input.DispatchMouseEventTypeMouseWheel, X: 400, Y: 300, DeltaX: 0.01, DeltaY: 1})
	must(err)
	time.Sleep(300 * time.Millisecond)
	check(eval[bool](ctx, `!document.querySelector('.item.current')`), "Esc, then a scroll: still nothing outlined")
	press("j")
	check(waitFor(ctx, current+` === 'Key item 5'`, 3*time.Second), "J: outlined again (%q)", eval[string](ctx, current))

	// A tap does not end the outline: it helps reading on phones too.
	_, err = chromedp.Call(ctx, emulation.SetTouchEmulationEnabled, emulation.SetTouchEmulationEnabledParams{Enabled: true, MaxTouchPoints: 5})
	must(err)
	tapAt := eval[[]float64](ctx, `(() => {
		const text = document.querySelectorAll('.item')[4].querySelector('.content').getBoundingClientRect();
		return [text.left + 30, Math.max(text.top, 80) + 10];
	})()`)
	tap(ctx, tapAt[0], tapAt[1])
	time.Sleep(300 * time.Millisecond)
	check(eval[bool](ctx, `document.querySelector('.item.current') !== null`), "tap: still outlined")
	_, err = chromedp.Call(ctx, emulation.SetTouchEmulationEnabled, emulation.SetTouchEmulationEnabledParams{Enabled: false})
	must(err)

	// After a click, the outline follows the scroll, as after J.
	at5 = eval[[]float64](ctx, `(() => {
		const text = document.querySelectorAll('.item')[4].querySelector('.content').getBoundingClientRect();
		return [text.left + 30, Math.max(text.top, 80) + 10];
	})()`)
	must(chromedp.Do(ctx, chromedp.MouseClickXY(at5[0], at5[1])))
	check(waitFor(ctx, current+` === 'Key item 5'`, 2*time.Second), "click: outlined (%q)", eval[string](ctx, current))
	eval[bool](ctx, `(() => {
		const next = document.querySelectorAll('.items > [data-id]')[5];
		window.scrollTo(0, scrollY + next.getBoundingClientRect().top - 12);
		return true;
	})()`)
	check(waitFor(ctx, current+` === 'Key item 6'`, 2*time.Second), "scrolled past the item clicked: the next one is outlined (%q)", eval[string](ctx, current))
	scrolled := eval[float64](ctx, `scrollY`)
	press(" ")
	check(waitFor(ctx, fmt.Sprintf(`scrollY > %f`, scrolled), 2*time.Second), "Space: further down")
	// Back to the previous item if it starts within a screen, else a page up:
	// either way, up (items here are longer than the test's window).
	down := eval[float64](ctx, `scrollY`)
	press(" ", shift)
	check(waitFor(ctx, fmt.Sprintf(`scrollY < %f && scrollY >= %f`, down, scrolled-1), 2*time.Second),
		"Shift Space: back up (%v, from %v)", eval[float64](ctx, `scrollY`), down)
	press("j")
	cur := eval[string](ctx, current)
	tabs := eval[int](ctx, `window.__test.opened.length`)
	press(kb.ArrowRight)
	check(waitFor(ctx, fmt.Sprintf(`window.__test.opened.length === %d && window.__test.opened.at(-1) === %q`,
		tabs+1, site+"k"+strings.TrimPrefix(cur, "Key item ")), 2*time.Second), "→: the current item (%s) opens", cur)

	// Enter folds the current item, and unfolds it. Long items do not fold
	// on their own on a computer.
	const curFolded = `!!document.querySelector('.item.current .content.folded')`
	check(eval[bool](ctx, `!document.querySelector('.content.folded')`), "computer: no item folded on its own")
	press(kb.Enter)
	check(waitFor(ctx, curFolded+` === true`, 2*time.Second), "Enter: the current item folded")
	check(waitFor(ctx, `document.querySelector('.item.current').getBoundingClientRect().top >= 0`, 2*time.Second), "Enter: its top in view")
	press(kb.Enter)
	check(waitFor(ctx, curFolded+` === false`, 2*time.Second), "Enter again: unfolded")

	// U switches between Unread and All (the reading step left All on).
	const showing = `(localStorage.getItem('fever.showRead') === '1' ? 'All' : 'Unread')`
	was := eval[string](ctx, showing)
	press("u")
	check(waitFor(ctx, fmt.Sprintf(`%s !== '' && %s !== %q`, showing, showing, was), 5*time.Second), "U: from %s to the other", was)
	press("u")
	check(waitFor(ctx, fmt.Sprintf(`%s === %q`, showing, was), 5*time.Second), "U again: back to %s", was)

	// The list's menu: the arrow keys move between its items, and Tab out
	// of it closes it.
	const focusedItem = `(document.activeElement?.getAttribute('role') === 'menuitem' ? document.activeElement.textContent.trim() : '')`
	eval[bool](ctx, `([...document.querySelectorAll('.list-menu > button')].find(b => b.offsetParent).click(), true)`)
	check(waitFor(ctx, focusedItem+` !== ''`, 2*time.Second), "menu: its first item focused")
	first := eval[string](ctx, focusedItem)
	press(kb.ArrowDown)
	check(eval[bool](ctx, fmt.Sprintf(`%s !== '' && %s !== %q`, focusedItem, focusedItem, first)), "menu: ↓ to the next item (%q)", eval[string](ctx, focusedItem))
	press(kb.Home)
	check(eval[string](ctx, focusedItem) == first, "menu: Home back to the first item")
	press(kb.End)
	press(kb.Tab)
	check(waitFor(ctx, `!document.querySelector('.list-menu [role="menu"]')`, 2*time.Second), "menu: Tab out of it closes it")

	press("?")
	check(waitFor(ctx, dialog+` && document.querySelector('dialog[open]').textContent.includes('Keyboard shortcuts')`, 2*time.Second), "?: the list of shortcuts")
	press(kb.Escape)
	check(waitFor(ctx, `!`+dialog, 2*time.Second), "Esc: the list closes")

	refreshed := fmt.Sprintf(`window.__test.bodies.includes('/api/refresh {"feedId":%d,"groupId":null}')`, id)
	eval[bool](ctx, `(window.__test.bodies.length = 0, true)`)
	press("R", shift)
	check(waitFor(ctx, refreshed, 3*time.Second), "Shift R: the feed shown is refreshed")
	before := eval[string](ctx, `document.querySelector('.shell').className`)
	press("f")
	check(waitFor(ctx, fmt.Sprintf(`document.querySelector('.shell').className !== %q`, before), 2*time.Second), "F: the feed list shown or hidden")
	press(kb.Escape)

	// J past the loaded items while the next page is on its way: the last
	// item stays in view, and J then goes on into that page.
	eval[bool](ctx, `(() => {
		window.__test.slowPages = true;
		const item = document.querySelectorAll('.items > [data-id]')[18];
		window.scrollTo(0, scrollY + item.getBoundingClientRect().top - 12);
		return true;
	})()`)
	press("j")
	check(waitFor(ctx, current+` === 'Key item 20'`, 3*time.Second), "J: to the last loaded item (%q)", eval[string](ctx, current))
	press("j")
	check(waitFor(ctx, `document.querySelectorAll('.item').length === 25`, 6*time.Second), "the next page comes")
	press("j")
	check(waitFor(ctx, current+` === 'Key item 21'`, 3*time.Second), "J past the loaded items: on to the next page's first item (%q)", eval[string](ctx, current))
	press("j")
	check(waitFor(ctx, current+` === 'Key item 22'`, 3*time.Second), "J: and the next (%q)", eval[string](ctx, current))
	eval[bool](ctx, `(window.__test.slowPages = false, true)`)

	press("i")
	check(waitFor(ctx, at(feed+"/edit"), 3*time.Second), "I: the feed's settings")
	eval[bool](ctx, `(window.__test.bodies.length = 0, true)`)
	press("R", shift)
	check(waitFor(ctx, refreshed, 3*time.Second), "Shift R in the feed's settings: that feed is refreshed")
	press("2")
	check(waitFor(ctx, at("/"), 3*time.Second), "2: all items")
	check(waitFor(ctx, `document.querySelectorAll('.item').length > 0 && !document.querySelector('.item.current')`, 3*time.Second),
		"another list: nothing outlined at first")
	_, err = chromedp.Call(ctx, input.DispatchMouseEvent, input.DispatchMouseEventParams{Type: input.DispatchMouseEventTypeMouseWheel, X: 400, Y: 300, DeltaX: 0.01, DeltaY: 200}) // both deltas, and a zero one is left out
	must(err)
	check(waitFor(ctx, `document.querySelector('.item.current') !== null`, 3*time.Second), "the first scroll with the wheel: the current item outlined (pointer: %s)",
		eval[string](ctx, `['fine', 'coarse', 'none'].filter(p => matchMedia('(pointer: ' + p + ')').matches).join() + ' any: ' + ['fine', 'coarse', 'none'].filter(p => matchMedia('(any-pointer: ' + p + ')').matches).join()`))
	press("3")
	check(waitFor(ctx, at("/saved"), 3*time.Second), "3: saved items")
	press("5")
	check(waitFor(ctx, at("/search")+` && document.activeElement?.name === 'q'`, 3*time.Second), "5: search, ready to type in")
	press(kb.Escape)
	check(waitFor(ctx, `document.activeElement?.name !== 'q'`, 2*time.Second), "Esc: out of the search field")
	press("q") // not a shortcut: nothing happens
	press("/")
	check(waitFor(ctx, `document.activeElement?.name === 'q'`, 2*time.Second), "/: into the search field")
	press(kb.Escape)
	press("p")
	check(waitFor(ctx, at("/settings"), 3*time.Second), "P: settings")
	press("n")
	check(waitFor(ctx, at("/feeds/new"), 3*time.Second), "N: add a feed")
	eval[bool](ctx, `(document.activeElement?.blur(), true)`)
	press("N", shift)
	check(waitFor(ctx, at("/groups/new"), 3*time.Second), "Shift N: add a group")

	// On a phone, long items fold on their own, and items take the width of
	// the screen.
	_, err = chromedp.Call(ctx, emulation.SetDeviceMetricsOverride, emulation.SetDeviceMetricsOverrideParams{Width: 390, Height: 844, DeviceScaleFactor: 3, Mobile: true})
	must(err)
	navigate(ctx, base+feed)
	const folded = `(n) => !!document.querySelectorAll('.item')[n]?.querySelector('.content.folded')`
	check(waitFor(ctx, `(`+folded+`)(0)`, 10*time.Second), "phone: a long item folded")
	check(eval[bool](ctx, `document.querySelector('.item').getBoundingClientRect().height < innerHeight`), "phone: shorter than the screen once folded")
	check(eval[bool](ctx, `(() => {
		const box = document.querySelector('.item').getBoundingClientRect();
		return box.left === 0 && box.width === innerWidth;
	})()`), "phone: the item as wide as the screen")
	unfold := eval[[]float64](ctx, `(() => {
		const button = document.querySelector('.item .unfold button');
		button.scrollIntoView({ block: 'center' });
		const box = button.getBoundingClientRect();
		return [box.left + box.width / 2, box.top + box.height / 2];
	})()`)
	must(chromedp.Do(ctx, chromedp.MouseClickXY(unfold[0], unfold[1])))
	check(waitFor(ctx, `!(`+folded+`)(0)`, 2*time.Second), "phone: Continue reading unfolds it")
	time.Sleep(300 * time.Millisecond)
	check(eval[bool](ctx, `!(`+folded+`)(0) && (`+folded+`)(1)`), "phone: it stays unfolded, the next one is folded")

	// A touch phone has no list menu where nothing in it applies.
	const menuButton = `[...document.querySelectorAll('.list-menu > button')].some(b => b.offsetParent)`
	_, err = chromedp.Call(ctx, emulation.SetTouchEmulationEnabled, emulation.SetTouchEmulationEnabledParams{Enabled: true, MaxTouchPoints: 5})
	must(err)
	check(waitFor(ctx, menuButton, 3*time.Second), "touch phone, in a feed: the list menu")
	navigate(ctx, base+"/saved")
	check(waitFor(ctx, readerShown+` && !`+menuButton, 10*time.Second), "touch phone, saved items: no empty list menu")
	_, err = chromedp.Call(ctx, emulation.SetTouchEmulationEnabled, emulation.SetTouchEmulationEnabledParams{Enabled: false})
	must(err)
	_, err = chromedp.Call(ctx, emulation.ClearDeviceMetricsOverride, cdp.Empty{})
	must(err)

	// Marking read, and Z.
	feedUnread := `fetch('/api/subscriptions').then(r => r.json()).then(s => s.feeds.find(f => f.id === ` + fmt.Sprint(id) + `)?.unread)`
	navigate(ctx, base+feed)
	check(waitFor(ctx, `document.querySelectorAll('.item').length > 0`, 10*time.Second), "back on the feed")
	if eval[string](ctx, showing) != "Unread" {
		press("u")
		check(waitFor(ctx, showing+` === 'Unread'`, 5*time.Second), "U: unread items only")
	}
	unreadBefore := eval[int](ctx, feedUnread)
	press("a") // confirmed by the instrument script
	check(waitFor(ctx, feedUnread+`.then(n => n === 0)`, 5*time.Second), "A: the feed shown is read")
	check(waitFor(ctx, `document.querySelector('.empty')?.textContent.includes('No unread items') ?? false`, 5*time.Second), "A: no unread items left in the list")
	press("z")
	check(waitFor(ctx, `document.querySelectorAll('.item').length === 20 && !document.querySelector('.item.read')`, 5*time.Second),
		"Z after A: the items come back in the list")
	// Items read by J a few seconds before may come back too.
	check(waitFor(ctx, fmt.Sprintf(`%s.then(n => n >= %d)`, feedUnread, unreadBefore), 5*time.Second), "Z after A: unread again on the server")

	press("V", shift)
	check(waitFor(ctx, opened(site), 2*time.Second), "Shift V: the site opens")
	check(waitFor(ctx, `document.querySelectorAll('.item.read').length === 20`, 5*time.Second) && waitFor(ctx, feedUnread+`.then(n => n === 0)`, 5*time.Second),
		"Shift V: and the feed is read")
	press("z")
	check(waitFor(ctx, `!document.querySelector('.item.read')`, 5*time.Second), "Z: unread again")

	press("A", shift)
	check(waitFor(ctx, `fetch('/api/subscriptions').then(r => r.json()).then(s => s.totalUnread === 0)`, 5*time.Second), "Shift A: everything is read")
	check(eval[bool](ctx, noAlerts), "keyboard shortcuts: no error message")

	status := eval[int](ctx, fmt.Sprintf(`fetch('/api/feeds/%d', { method: 'DELETE' }).then(r => r.status)`, id))
	check(status == 204, "keys feed removed (%d)", status)
}
