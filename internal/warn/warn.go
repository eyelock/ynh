// Package warn is the one way ynh and ynd print a warning.
//
// A warning is a Warning, a message and the fix that clears it, not a plain
// string. Print and PrintNumbered choose how it reads from the stream they
// write to:
//
//   - a terminal: a red box, a blank line above and below;
//   - a terminal with NO_COLOR set: the same box, uncoloured;
//   - anything else (piped, a file, CI): plain lines, "warning: <message>"
//     then "  Fix: <fix>" when there is one, with no box characters, so logs
//     stay greppable. A message of several lines repeats "warning: " on each
//     line, so grepping a log for "warning:" misses none of it.
//
// Box layout: text wraps at spaces to the box width. A word longer than the
// width (a path, a URL) is cut at the width, so a box never outgrows the
// terminal. A Fix that fits on one line is "Fix: <fix>"; one that does not
// puts "Fix:" on its own line and the command below it, kept whole unless the
// command alone is longer than the width. Width is counted in runes: the box
// assumes one terminal cell per rune, so wide characters (CJK, emoji) can draw
// a ragged right border.
package warn

import (
	"fmt"
	"io"
	"os"
	"strings"
	"unicode/utf8"

	"golang.org/x/term"
)

// Warning is a message and the fix that clears it. Fix is empty only when no
// known command or edit clears the warning.
type Warning struct {
	Message string `json:"message"`
	Fix     string `json:"fix"`
}

// Mode is how a warning is drawn.
type Mode int

const (
	// Plain is "warning: <message>" and "  Fix: <fix>" lines, no box.
	Plain Mode = iota
	// Box is an uncoloured box.
	Box
	// ColorBox is a red box.
	ColorBox
)

const (
	red   = "\x1b[31m"
	reset = "\x1b[0m"

	// maxWidth caps a box's inner text, so a long message wraps instead of
	// running off a wide terminal.
	maxWidth = 76
	// minWidth keeps a box readable on a very narrow terminal.
	minWidth = 20
)

// ModeFor reports how a warning written to w is drawn: a red box on a
// terminal, an uncoloured one when NO_COLOR is set (any non-empty value, per
// no-color.org), plain lines otherwise.
func ModeFor(w io.Writer) Mode {
	f, ok := w.(*os.File)
	return modeFor(ok && term.IsTerminal(int(f.Fd())), os.Getenv("NO_COLOR"))
}

func modeFor(isTerminal bool, noColor string) Mode {
	switch {
	case !isTerminal:
		return Plain
	case noColor != "":
		return Box
	default:
		return ColorBox
	}
}

// Print writes each warning to w in the form w calls for.
func Print(w io.Writer, ws ...Warning) {
	Render(w, ModeFor(w), false, ws...)
}

// PrintNumbered writes the warnings to w as one run, "warning 1 of 2", for
// the end of ynh run's setup. A single warning is numbered too, so the run
// reads the same whatever its length.
func PrintNumbered(w io.Writer, ws ...Warning) {
	Render(w, ModeFor(w), true, ws...)
}

// Render writes the warnings to w in mode. Print and PrintNumbered choose the
// mode from the stream; Render is for a caller that already knows it, and for
// tests.
func Render(w io.Writer, mode Mode, numbered bool, ws ...Warning) {
	for i, x := range ws {
		label := "warning"
		if numbered {
			label = fmt.Sprintf("warning %d of %d", i+1, len(ws))
		}
		if mode == Plain {
			writePlain(w, label, x)
			continue
		}
		writeBox(w, label, x, mode == ColorBox, boxWidth(w))
	}
}

func writePlain(w io.Writer, label string, x Warning) {
	for _, line := range strings.Split(x.Message, "\n") {
		_, _ = fmt.Fprintf(w, "%s: %s\n", label, line)
	}
	if x.Fix != "" {
		_, _ = fmt.Fprintf(w, "  Fix: %s\n", x.Fix)
	}
}

func boxWidth(w io.Writer) int {
	width := maxWidth
	if f, ok := w.(*os.File); ok {
		if cols, _, err := term.GetSize(int(f.Fd())); err == nil && cols > 0 {
			// Two border cells and a space of padding each side.
			width = min(width, cols-4)
		}
	}
	return max(width, minWidth)
}

func writeBox(w io.Writer, label string, x Warning, color bool, width int) {
	lines := []string{label, ""}
	lines = append(lines, wrap(x.Message, width)...)
	if x.Fix != "" {
		lines = append(lines, "")
		lines = append(lines, fixLines(x.Fix, width)...)
	}
	inner := 0
	for _, l := range lines {
		inner = max(inner, utf8.RuneCountInString(l))
	}
	paint := func(s string) string {
		if color {
			return red + s + reset
		}
		return s
	}
	var b strings.Builder
	b.WriteString("\n")
	b.WriteString(paint("┌"+strings.Repeat("─", inner+2)+"┐") + "\n")
	for _, l := range lines {
		pad := strings.Repeat(" ", inner-utf8.RuneCountInString(l))
		b.WriteString(paint("│") + " " + l + pad + " " + paint("│") + "\n")
	}
	b.WriteString(paint("└"+strings.Repeat("─", inner+2)+"┘") + "\n")
	b.WriteString("\n")
	_, _ = io.WriteString(w, b.String())
}

// fixLines lays out a Fix: on one line when "Fix: <fix>" fits, otherwise
// "Fix:" then the command on its own line, so the command a user copies is
// not split at a space. Only a command longer than width is cut.
func fixLines(fix string, width int) []string {
	one := "Fix: " + fix
	if !strings.Contains(fix, "\n") && utf8.RuneCountInString(one) <= width {
		return []string{one}
	}
	out := []string{"Fix:"}
	for _, l := range strings.Split(fix, "\n") {
		out = append(out, cut(l, width)...)
	}
	return out
}

// cut splits s into pieces of at most width runes.
func cut(s string, width int) []string {
	r := []rune(s)
	if len(r) <= width {
		return []string{s}
	}
	var out []string
	for len(r) > width {
		out = append(out, string(r[:width]))
		r = r[width:]
	}
	return append(out, string(r))
}

// wrap breaks s into lines of at most width runes at spaces. A newline in s
// starts a new line; a word longer than width is cut at width.
func wrap(s string, width int) []string {
	var out []string
	for _, para := range strings.Split(s, "\n") {
		line := ""
		for _, word := range strings.Fields(para) {
			for utf8.RuneCountInString(word) > width {
				if line != "" {
					out = append(out, line)
					line = ""
				}
				c := cut(word, width)
				out = append(out, c[0])
				word = strings.Join(c[1:], "")
			}
			switch {
			case line == "":
				line = word
			case utf8.RuneCountInString(line)+1+utf8.RuneCountInString(word) <= width:
				line += " " + word
			default:
				out = append(out, line)
				line = word
			}
		}
		out = append(out, line)
	}
	return out
}
