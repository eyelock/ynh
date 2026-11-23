package warn

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

var one = Warning{Message: "hooks are not active", Fix: `add "hooks": true to the include`}

func TestRender_Plain(t *testing.T) {
	var b bytes.Buffer
	Render(&b, Plain, false, one, Warning{Message: "no fix here"})
	want := "warning: hooks are not active\n  Fix: add \"hooks\": true to the include\nwarning: no fix here\n"
	if b.String() != want {
		t.Errorf("got %q, want %q", b.String(), want)
	}
	if strings.ContainsAny(b.String(), "┌│└─\x1b") {
		t.Error("plain output carries box or colour characters")
	}
}

func TestRender_PlainNumbered(t *testing.T) {
	var b bytes.Buffer
	Render(&b, Plain, true, one, Warning{Message: "second"})
	want := "warning 1 of 2: hooks are not active\n  Fix: add \"hooks\": true to the include\nwarning 2 of 2: second\n"
	if b.String() != want {
		t.Errorf("got %q, want %q", b.String(), want)
	}
}

func TestRender_Box(t *testing.T) {
	var b bytes.Buffer
	Render(&b, Box, false, one)
	got := b.String()
	if strings.Contains(got, "\x1b") {
		t.Error("uncoloured box carries colour codes")
	}
	if !strings.HasPrefix(got, "\n┌") || !strings.HasSuffix(got, "┘\n\n") {
		t.Errorf("box lacks a blank line above and below: %q", got)
	}
	for _, want := range []string{"warning", "hooks are not active", "Fix: add"} {
		if !strings.Contains(got, want) {
			t.Errorf("box lacks %q:\n%s", want, got)
		}
	}
	assertRectangular(t, got)
}

func TestRender_BoxNoFixHasNoFixLine(t *testing.T) {
	var b bytes.Buffer
	Render(&b, Box, false, Warning{Message: "just a message"})
	if strings.Contains(b.String(), "Fix:") {
		t.Errorf("a warning with no fix prints a Fix line:\n%s", b.String())
	}
}

func TestRender_ColorBoxIsRed(t *testing.T) {
	var b bytes.Buffer
	Render(&b, ColorBox, false, one)
	if !strings.Contains(b.String(), red+"┌") || !strings.Contains(b.String(), reset) {
		t.Errorf("colour box is not red:\n%q", b.String())
	}
}

func TestRender_BoxNumbered(t *testing.T) {
	var b bytes.Buffer
	Render(&b, Box, true, one, Warning{Message: "second"})
	got := b.String()
	if !strings.Contains(got, "warning 1 of 2") || !strings.Contains(got, "warning 2 of 2") {
		t.Errorf("numbered boxes lack their counts:\n%s", got)
	}
}

func TestRender_BoxWrapsLongMessage(t *testing.T) {
	var b bytes.Buffer
	Render(&b, Box, false, Warning{Message: strings.Repeat("word ", 40)})
	assertRectangular(t, b.String())
	for _, l := range strings.Split(b.String(), "\n") {
		if n := len([]rune(l)); n > maxWidth+4 {
			t.Errorf("line of %d runes exceeds the cap: %q", n, l)
		}
	}
}

func assertRectangular(t *testing.T, s string) {
	t.Helper()
	width := -1
	for _, l := range strings.Split(strings.Trim(s, "\n"), "\n") {
		n := len([]rune(l))
		if width < 0 {
			width = n
		} else if n != width {
			t.Errorf("box is ragged: %d vs %d in %q", n, width, l)
		}
	}
}

func TestModeFor_NotATerminal(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	if got := ModeFor(&bytes.Buffer{}); got != Plain {
		t.Errorf("buffer: got %v, want Plain", got)
	}
	f, err := os.CreateTemp(t.TempDir(), "w")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	if got := ModeFor(f); got != Plain {
		t.Errorf("file: got %v, want Plain", got)
	}
}

func TestPrint_ToBufferIsPlain(t *testing.T) {
	var b bytes.Buffer
	Print(&b, one)
	PrintNumbered(&b, one)
	want := "warning: hooks are not active\n  Fix: add \"hooks\": true to the include\n" +
		"warning 1 of 1: hooks are not active\n  Fix: add \"hooks\": true to the include\n"
	if b.String() != want {
		t.Errorf("got %q, want %q", b.String(), want)
	}
}

func TestModeFor_Choice(t *testing.T) {
	cases := []struct {
		isTerminal bool
		noColor    string
		want       Mode
	}{
		{true, "", ColorBox},
		{true, "1", Box},
		{false, "", Plain},
		{false, "1", Plain},
	}
	for _, c := range cases {
		if got := modeFor(c.isTerminal, c.noColor); got != c.want {
			t.Errorf("modeFor(%v, %q) = %v, want %v", c.isTerminal, c.noColor, got, c.want)
		}
	}
}

func TestRender_BoxKeepsLongFixCommandWhole(t *testing.T) {
	cmd := "ynh trust accept local/a-rather-long-harness-name --and --some --flags"
	var b bytes.Buffer
	Render(&b, Box, false, Warning{Message: "needs trust", Fix: cmd})
	assertRectangular(t, b.String())
	out := b.String()
	if !strings.Contains(out, "│ Fix:") {
		t.Errorf("a Fix that does not fit one line gets its own label line:\n%s", out)
	}
	if !strings.Contains(out, cmd) {
		t.Errorf("the Fix command was split across lines:\n%s", out)
	}
}

func TestRender_BoxShortFixStaysOnOneLine(t *testing.T) {
	var b bytes.Buffer
	Render(&b, Box, false, Warning{Message: "m", Fix: "ynh trust accept local/x"})
	if !strings.Contains(b.String(), "Fix: ynh trust accept local/x") {
		t.Errorf("short fix should read 'Fix: <cmd>':\n%s", b.String())
	}
}

func TestRender_BoxCutsWordLongerThanWidth(t *testing.T) {
	long := strings.Repeat("z", 3*maxWidth)
	var b bytes.Buffer
	Render(&b, Box, false, Warning{Message: "see " + long, Fix: "run " + long})
	assertRectangular(t, b.String())
	for _, l := range strings.Split(b.String(), "\n") {
		if n := len([]rune(l)); n > maxWidth+4 {
			t.Errorf("line of %d runes exceeds the cap: %q", n, l)
		}
	}
	if got := strings.Count(b.String(), "z"); got != 6*maxWidth {
		t.Errorf("cutting lost or added characters: %d z's, want %d", got, 6*maxWidth)
	}
}

func TestRender_PlainPrefixesEveryMessageLine(t *testing.T) {
	var b bytes.Buffer
	Render(&b, Plain, false, Warning{Message: "line one\nline two", Fix: "do it"})
	want := "warning: line one\nwarning: line two\n  Fix: do it\n"
	if b.String() != want {
		t.Errorf("got %q, want %q", b.String(), want)
	}
}
