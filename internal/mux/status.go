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

func statusText(text string) string {
	var out strings.Builder
	for len(text) > 0 {
		seq, _, n, _ := ansi.DecodeSequence(text, 0, nil)
		text = text[n:]
		if seq == accent || seq == "\x1b[39m" {
			out.WriteString(seq)
		} else {
			out.WriteString(cleanText(seq))
		}
	}
	return out.String()
}

func (s *Server) statusLine(session *session, cols int) (string, error) {
	data := struct {
		Windows []windowInfo
		Session struct{ Name, ID string }
	}{Windows: s.windowInfos(session)}
	data.Session.Name, data.Session.ID = session.name, session.name
	var sections [3]string
	for i, t := range s.config.statusTemplates {
		if t == nil {
			return "", fmt.Errorf("status templates are not initialized")
		}
		var out statusWriter
		if err := t.Execute(&out, data); err != nil {
			return "", fmt.Errorf("%s: %w", t.Name(), err)
		}
		sections[i] = statusText(out.text.String())
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
	return "\x1b[?2027h\x1b[0m" + left + "\x1b[0m" +
		strings.Repeat(" ", start-leftWidth) + center + "\x1b[0m" +
		strings.Repeat(" ", rightX-start-ansi.StringWidth(center)) + right + "\x1b[0m", nil
}
