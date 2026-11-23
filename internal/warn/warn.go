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
//     stay greppable.
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
	_, _ = fmt.Fprintf(w, "%s: %s\n", label, x.Message)
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
		lines = append(lines, wrap("Fix: "+x.Fix, width)...)
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

// wrap breaks s into lines of at most width runes at spaces. A newline in s
// starts a new line; a word longer than width stays whole.
func wrap(s string, width int) []string {
	var out []string
	for _, para := range strings.Split(s, "\n") {
		line := ""
		for _, word := range strings.Fields(para) {
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
