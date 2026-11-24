package main

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
)

// designTokens reads docs/design/tokens.json: every token's value by
// name, a color's per theme ("light", "dark"; a plain value is both).
func designTokens(t *testing.T) map[string]map[string]string {
	t.Helper()
	b, err := os.ReadFile("docs/design/tokens.json")
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]json.RawMessage
	if err = json.Unmarshal(b, &doc); err != nil {
		t.Fatal(err)
	}
	out := map[string]map[string]string{}
	for family, raw := range doc {
		var f struct {
			Tokens []struct {
				Name  string          `json:"name"`
				Value json.RawMessage `json:"value"`
			} `json:"tokens"`
		}
		if family == "type" || family == "meta" || json.Unmarshal(raw, &f) != nil {
			continue
		}
		for _, tok := range f.Tokens {
			var plain string
			var themed map[string]string
			switch {
			case json.Unmarshal(tok.Value, &plain) == nil:
				out[tok.Name] = map[string]string{"light": plain, "dark": plain}
			case json.Unmarshal(tok.Value, &themed) == nil:
				out[tok.Name] = themed
			default:
				t.Fatalf("token %s: a value neither a string nor per theme", tok.Name)
			}
		}
	}
	return out
}

// parseColor reads a token's #rrggbb or rgba(r, g, b, a).
func parseColor(s string) (r, g, b uint8, a float64, err error) {
	if h, ok := strings.CutPrefix(s, "#"); ok && len(h) == 6 {
		v, err := strconv.ParseUint(h, 16, 32)
		return uint8(v >> 16), uint8(v >> 8), uint8(v), 1, err
	}
	var ri, gi, bi int
	if _, err = fmt.Sscanf(s, "rgba(%d, %d, %d, %g)", &ri, &gi, &bi, &a); err != nil {
		return 0, 0, 0, 0, fmt.Errorf("%q: %w", s, err)
	}
	return uint8(ri), uint8(gi), uint8(bi), a, nil
}

// TestDesignTokensMatchCode checks that the design system's tokens say
// what the code draws: the colors, looks, sizes and glass parameters it
// repeats from it. Change one, change the other.
func TestDesignTokensMatchCode(t *testing.T) {
	tokens := designTokens(t)
	color := func(name, theme string, want ui.Color) {
		t.Helper()
		v, ok := tokens[name][theme]
		if !ok {
			t.Errorf("%s (%s): no such token", name, theme)
			return
		}
		r, g, b, a, err := parseColor(v)
		if err != nil {
			t.Errorf("%s (%s): %v", name, theme, err)
			return
		}
		if r != want.R || g != want.G || b != want.B || math.Abs(a*255-float64(want.A)) > 1 {
			t.Errorf("%s (%s) is %s; the code draws rgba(%d, %d, %d, %.3f)", name, theme, v, want.R, want.G, want.B, float64(want.A)/255)
		}
	}
	value := func(name, want string) {
		t.Helper()
		if got := tokens[name]["light"]; got != want {
			t.Errorf("%s is %q; the code has %q", name, got, want)
		}
	}
	light, dark := ui.LightTheme(), ui.DarkTheme()
	themes := map[string]*ui.Theme{"light": light, "dark": dark}

	for _, ac := range accents {
		if ac.color.A > 0 {
			color("accent-"+ac.key, "light", ac.color)
		}
	}
	for id, th := range themes {
		color("text", id, th.Text)
		color("text-muted", id, th.TextMuted)
		color("background", id, th.Background)
		color("success", id, th.Success)
		color("warning", id, th.Warning)
		color("danger", id, th.Danger)
		color("solid-background", id, solidBackground(th))
		color("solid-pane", id, solidPane(th))
		color("solid-raised", id, solidRaised(th))
	}
	for name, l := range map[string]*look{"standard": lookFor(""), "max": lookFor("max")} {
		for id, th := range themes {
			color("backdrop-tone-"+name, id, l.backdropTone(th))
			color("content-"+name, id, l.contentTint(th))
			color("edge-tone-"+name, id, l.edgeTone(th))
		}
		value("blur-backdrop-"+name, fmt.Sprintf("%gpx", l.blur))
		value("clarity-sidebar-"+name, fmt.Sprintf("%g", l.pane.Clarity))
	}
	value("blur-content-max", fmt.Sprintf("%gpx", lookFor("max").contentBlur))
	color("media-dim", "light", mediaGlass.Tint)
	value("titlebar-height", fmt.Sprintf("%dpx", titleBarHeight))
	value("header-height", fmt.Sprintf("%dpx", headerHeight))
	value("sidebar-width", fmt.Sprintf("%dpx", sidebarWidth))
	value("drag-threshold", fmt.Sprintf("%dpx", dragThreshold))
	value("drag-frame", fmt.Sprintf("%dms", dragFrame.Milliseconds()))
}

func relativeLuminance(c ui.Color) float64 {
	lin := func(u uint8) float64 {
		s := float64(u) / 255
		if s <= 0.03928 {
			return s / 12.92
		}
		return math.Pow((s+0.055)/1.055, 2.4)
	}
	return 0.2126*lin(c.R) + 0.7152*lin(c.G) + 0.0722*lin(c.B)
}

func contrastRatio(a, b ui.Color) float64 {
	la, lb := relativeLuminance(a), relativeLuminance(b)
	if la < lb {
		la, lb = lb, la
	}
	return (la + 0.05) / (lb + 0.05)
}

func TestAccentContrast(t *testing.T) {
	base := ui.LightTheme()
	for _, ac := range accents {
		if ac.color.A == 0 {
			continue
		}
		fg := base.WithAccent(ac.color).AccentText
		if ratio := contrastRatio(ac.color, fg); ratio < 3.0 {
			t.Errorf("accent %s (%s) contrast with on-accent is %.2f:1, want >= 3.0:1", ac.key, ac.label, ratio)
		}
	}
}
