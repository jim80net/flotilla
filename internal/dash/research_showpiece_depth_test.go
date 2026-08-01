package dash

import (
	"math"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

func TestResearchShowpieceDepthBodyContrast(t *testing.T) {
	raw, err := os.ReadFile("../../docs/examples/research-showpiece-depth.css")
	if err != nil {
		t.Fatal(err)
	}
	css := string(raw)
	pairs := []struct {
		name, foreground, background string
	}{
		{"light panel", "--presentation-body-on-light", "--presentation-panel-light"},
		{"dark panel", "--presentation-body-on-dark", "--presentation-panel-dark"},
	}
	for _, pair := range pairs {
		fg := showpieceCSSHex(t, css, pair.foreground)
		bg := showpieceCSSHex(t, css, pair.background)
		if got := showpieceContrast(fg, bg); got < 7 {
			t.Errorf("%s body contrast = %.2f:1, want >= 7:1", pair.name, got)
		}
	}
	for _, use := range []string{
		"color: var(--presentation-body-on-light)",
		"color: var(--presentation-body-on-dark)",
		"background: var(--presentation-panel-light)",
		"background: var(--presentation-panel-dark)",
	} {
		if !strings.Contains(css, use) {
			t.Errorf("reference depth CSS does not consume semantic token %q", use)
		}
	}
}

func showpieceCSSHex(t *testing.T, css, name string) [3]float64 {
	t.Helper()
	re := regexp.MustCompile(regexp.QuoteMeta(name) + `\s*:\s*#([0-9a-fA-F]{6})`)
	match := re.FindStringSubmatch(css)
	if len(match) != 2 {
		t.Fatalf("CSS variable %s must be one six-digit hex color", name)
	}
	var rgb [3]float64
	for i := range rgb {
		value, err := strconv.ParseUint(match[1][i*2:i*2+2], 16, 8)
		if err != nil {
			t.Fatal(err)
		}
		rgb[i] = float64(value) / 255
	}
	return rgb
}

func showpieceContrast(a, b [3]float64) float64 {
	luminance := func(rgb [3]float64) float64 {
		for i, channel := range rgb {
			if channel <= 0.04045 {
				rgb[i] = channel / 12.92
			} else {
				rgb[i] = math.Pow((channel+0.055)/1.055, 2.4)
			}
		}
		return 0.2126*rgb[0] + 0.7152*rgb[1] + 0.0722*rgb[2]
	}
	la, lb := luminance(a), luminance(b)
	if la < lb {
		la, lb = lb, la
	}
	return (la + 0.05) / (lb + 0.05)
}
