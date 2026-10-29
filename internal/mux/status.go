package mux

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/x/ansi"
)

type statusWriter struct {
	text strings.Builder
}

func (w *statusWriter) Write(p []byte) (int, error) {
	if len(p) > 65536-w.text.Len() {
		return 0, fmt.Errorf("status template output exceeds 64 KiB")
	}
	return w.text.Write(p)
}

func statusText(text, highlight string) string {
	var out strings.Builder
	foregroundReset := ansi.SGR(ansi.AttrDefaultForegroundColor)
	colorReset := ansi.SGR(ansi.AttrDefaultForegroundColor, ansi.AttrDefaultBackgroundColor)
	for len(text) > 0 {
		seq, _, n, _ := ansi.DecodeSequence(text, 0, nil)
		text = text[n:]
		if seq == highlight || seq == foregroundReset || seq == colorReset {
			out.WriteString(seq)
		} else {
			out.WriteString(cleanText(seq))
		}
	}
	return out.String()
}

func (config Config) statusLine(windows []windowInfo, name string, cols int) (string, error) {
	data := struct {
		Windows []windowInfo
		Session struct{ Name, ID string }
	}{Windows: windows}
	data.Session.Name, data.Session.ID = name, name
	var sections [3]string
	for i, t := range config.statusTemplates {
		if t == nil {
			return "", fmt.Errorf("status templates are not initialized")
		}
		var out statusWriter
		if err := t.Execute(&out, data); err != nil {
			return "", fmt.Errorf("%s: %w", t.Name(), err)
		}
		sections[i] = statusText(out.text.String(), config.activeTabStyle)
	}
	right := ansi.Truncate(sections[2], cols, "")
	rightX := cols - ansi.StringWidth(right)
	left := ansi.Truncate(sections[0], rightX, "")
	leftWidth := ansi.StringWidth(left)
	centerWidth := ansi.StringWidth(sections[1])
	centerX := (cols - centerWidth) / 2
	start, end := max(leftWidth, centerX), min(rightX, centerX+centerWidth)
	center := ""
	if start < end {
		// Cut retains a wide grapheme crossing its left edge; skip that cell.
		if ansi.StringWidth(ansi.Truncate(sections[1], start-centerX, "")) < start-centerX {
			start++
		}
		if start < end {
			center = ansi.Cut(sections[1], start-centerX, end-centerX)
		}
	}
	if center == "" {
		start = leftWidth
	}
	reset := ansi.SGR(ansi.AttrReset)
	return ansi.SetModeUnicodeCore + reset + left + reset +
		strings.Repeat(" ", start-leftWidth) + center + reset +
		strings.Repeat(" ", rightX-start-ansi.StringWidth(center)) + right + reset, nil
}
