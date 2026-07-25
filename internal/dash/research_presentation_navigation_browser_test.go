package dash

import (
	"fmt"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestResearchPresentationNavigationRendered875 reproduces the tall-section
// mobile failure with a generic nine-section showpiece. It proves that the
// shared controller owns buttons/keys, derives wheel state from real overlap,
// preserves free scrolling, and reaches both ends.
func TestResearchPresentationNavigationRendered875(t *testing.T) {
	python := os.Getenv("FLOTILLA_PLAYWRIGHT_PYTHON")
	if python == "" {
		t.Skip("set FLOTILLA_PLAYWRIGHT_PYTHON to run rendered Research presentation regression")
	}
	if _, err := exec.LookPath(python); err != nil {
		t.Fatalf("playwright python: %v", err)
	}

	srv, _ := newTestServer(t, singleFleetRoster, time.Now())
	root := t.TempDir()
	srv.cfg.ResearchPath = root
	writeResearchFixture(t, root, "generic/SOURCE.md", "# Generic nine-section showpiece\n\nA public-safe navigation fixture.\n", time.Now())

	var sections strings.Builder
	for i := 1; i <= 9; i++ {
		height := ""
		if i == 4 {
			height = ` style="min-height:1800px"`
		}
		fmt.Fprintf(&sections, `<section class="slide" data-label="Section %02d"%s><h2>Visible section %02d</h2></section>`, i, height, i)
	}
	html := fmt.Sprintf(`<!doctype html>
<html><head><meta name="viewport" content="width=device-width"><title>Section 01 · Generic showpiece</title>
<style>
*{box-sizing:border-box}html,body{margin:0}main{height:100vh;overflow-y:auto;scroll-snap-type:none}
.slide{display:grid;place-content:center;min-height:100vh;padding:5rem 1rem;background:#f6f1e7;color:#14231f}
.slide:nth-child(even){background:#14231f;color:#f6f1e7}
.counter,.visible-label{position:fixed;bottom:12px;z-index:5;padding:8px;background:#14231f;color:white}
.counter{left:12px}.visible-label{left:92px}.controls{position:fixed;right:12px;bottom:8px;display:flex;gap:8px}
button{width:48px;height:48px}
</style></head><body>
<main>%s</main>
<div class="counter"><span data-current>01</span>/<span data-total>09</span></div>
<span class="visible-label" data-visible-label></span>
<nav class="controls"><button data-prev aria-label="Previous section">↑</button><button data-next aria-label="Next section">↓</button></nav>
<script>
// Deliberately reproduces the old impossible threshold for a tall phone section.
const oldDeck=document.querySelector("main"),oldSlides=[...document.querySelectorAll(".slide")];
let oldCurrent=0;
const oldObserver=new IntersectionObserver(entries=>{
 const visible=entries.filter(e=>e.isIntersecting).sort((a,b)=>b.intersectionRatio-a.intersectionRatio)[0];
 if(visible&&visible.intersectionRatio>=.55)oldCurrent=oldSlides.indexOf(visible.target);
},{root:oldDeck,threshold:[.55,.8]});
oldSlides.forEach(slide=>oldObserver.observe(slide));
document.querySelector("[data-next]").addEventListener("click",()=>oldSlides[Math.min(8,oldCurrent+1)].scrollIntoView({behavior:"smooth"}));
</script></body></html>`, sections.String())
	presentation := filepath.Join(root, "generic", "presentation", "index.html")
	if err := os.MkdirAll(filepath.Dir(presentation), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(presentation, []byte(html), 0o600); err != nil {
		t.Fatal(err)
	}

	httpServer := httptest.NewServer(srv.mux)
	t.Cleanup(httpServer.Close)

	script := `
import sys
import time
from playwright.sync_api import sync_playwright, expect

url = sys.argv[1]

def assert_state(frame, number):
    label = "Section %02d" % number
    expect(frame.locator("[data-current]")).to_have_text("%02d" % number)
    expect(frame.locator("[data-total]")).to_have_text("09")
    expect(frame.locator("[data-visible-label]")).to_have_text(label)
    assert frame.title() == label + " · Generic showpiece", (number, frame.title())
    visible = frame.locator(".slide").evaluate_all("""(slides) => {
      const deck = document.querySelector("main"), top = deck.scrollTop, bottom = top + deck.clientHeight;
      return slides.map((slide, index) => ({
        index: index + 1,
        overlap: Math.max(0, Math.min(bottom, slide.offsetTop + slide.offsetHeight) - Math.max(top, slide.offsetTop))
      })).sort((a, b) => b.overlap - a.overlap)[0].index;
    }""")
    assert visible == number, (number, visible)
    viewport = frame.locator("body").evaluate("() => ({width: innerWidth, height: innerHeight})")
    for selector in ["[data-prev]", "[data-next]"]:
        box = frame.locator(selector).evaluate("""node => {
          const box = node.getBoundingClientRect();
          return {x: box.x, y: box.y, width: box.width, height: box.height};
        }""")
        assert box and box["width"] >= 44 and box["height"] >= 44, (number, selector, box)
        assert box["x"] >= 0 and box["x"] + box["width"] <= viewport["width"], (number, selector, box)
        assert box["y"] >= 0 and box["y"] + box["height"] <= viewport["height"], (number, selector, box)
    assert frame.locator("[data-prev]").is_disabled() == (number == 1)
    assert frame.locator("[data-next]").is_disabled() == (number == 9)

def wait_at(frame, number):
    reached = False
    for _ in range(160):
        reached = frame.locator("main").evaluate("""(deck, number) => {
          const slide = document.querySelectorAll(".slide")[number - 1];
          return Math.abs(deck.scrollTop - slide.offsetTop) <= 2 ||
            (number === 9 && Math.abs(deck.scrollTop + deck.clientHeight - deck.scrollHeight) <= 2);
        }""", number)
        if reached:
            break
        time.sleep(.05)
    assert reached, ("unreached", number, frame.locator("main").evaluate("deck => ({top:deck.scrollTop,max:deck.scrollHeight-deck.clientHeight})"))
    assert_state(frame, number)

with sync_playwright() as p:
    browser = p.chromium.launch()
    page = browser.new_page(viewport={"width": 390, "height": 844})
    page.set_default_timeout(8000)
    page.add_init_script("window.EventSource = undefined")
    page.goto(url + "/research?focus=library", wait_until="domcontentloaded")
    page.locator("#research-list .research-card").filter(has_text="Generic nine-section").click()
    expect(page.locator("#research-presentation")).to_be_visible()
    page.locator("#research-presentation").scroll_into_view_if_needed()
    frame = page.frames[-1]
    expect(frame.locator("html")).to_have_attribute("data-flotilla-presentation-controller", "ready")
    assert frame.locator("main").evaluate("node => getComputedStyle(node).scrollSnapType") == "none"
    assert_state(frame, 1)

    frame.locator("[data-next]").click()
    wait_at(frame, 2)
    frame.locator("body").press("ArrowDown")
    wait_at(frame, 3)

    # Native wheel reaches section 4 and updates state without a snap mandate.
    frame.locator("main").hover()
    page.mouse.wheel(0, frame.locator("main").evaluate("node => node.clientHeight"))
    expect(frame.locator("[data-current]")).to_have_text("04")
    assert_state(frame, 4)
    page.mouse.wheel(0, 240)
    frame.wait_for_timeout(100)
    free = frame.locator("main").evaluate("""deck => ({
      top: deck.scrollTop,
      four: document.querySelectorAll('.slide')[3].offsetTop,
      five: document.querySelectorAll('.slide')[4].offsetTop
    })""")
    assert free["top"] > free["four"] and free["top"] < free["five"], free
    assert_state(frame, 4)

    for number in range(5, 10):
        frame.locator("[data-next]").click()
        wait_at(frame, number)

    frame.locator("[data-prev]").click()
    wait_at(frame, 8)
    frame.locator("body").press("ArrowUp")
    wait_at(frame, 7)
    for number in range(6, 0, -1):
        frame.locator("[data-prev]").click()
        wait_at(frame, number)

    browser.close()
`
	cmd := exec.Command(python, "-c", script, httpServer.URL)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("rendered Research presentation navigation regression: %v\n%s", err, out)
	}
}
