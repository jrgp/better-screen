package mux

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/lucasb-eyer/go-colorful"
)

type Config struct {
	StatusLine           *bool   `json:"status_line"`
	StatusLineLeft       *string `json:"status_line_left"`
	StatusLineCenter     *string `json:"status_line_center"`
	StatusLineRight      *string `json:"status_line_right"`
	BorderColor          *string `json:"border_color"`
	StatusActiveTabColor *string `json:"status_active_tab_color"`
	StatusActiveTabBg    *string `json:"status_active_tab_bg"`
	borderStyle          string
	activeTabStyle       string
	statusTemplates      [3]*template.Template
}

const defaultAccentColor = "#87cefa"

const defaultStatusLeft = `{{ range .Windows }}{{ if .Active }}{{ highlight (printf "[%s]" .Title) }}{{ else }}[{{ .Title }}]{{ end }} {{ end }}`

func loadConfig(home string) (Config, error) {
	// Omitted fields keep their defaults; explicit null values become nil.
	config := &Config{
		StatusLine:           new(true),
		StatusLineLeft:       new(defaultStatusLeft),
		StatusLineCenter:     new(""),
		StatusLineRight:      new("{{ .Session.Name }}"),
		BorderColor:          new(defaultAccentColor),
		StatusActiveTabColor: new(defaultAccentColor),
		StatusActiveTabBg:    new(""),
	}
	path := filepath.Join(home, ".bs", "config.json")
	f, err := os.Open(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return Config{}, err
	}
	if err == nil {
		defer f.Close()
		data, err := io.ReadAll(io.LimitReader(f, 65537))
		if err != nil {
			return Config{}, err
		}
		if len(data) > 65536 {
			return Config{}, fmt.Errorf("%s: configuration exceeds 64 KiB", path)
		}
		decoder := json.NewDecoder(bytes.NewReader(data))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&config); err != nil {
			return Config{}, fmt.Errorf("%s: %w", path, err)
		}
		if err := decoder.Decode(new(any)); err != io.EOF {
			return Config{}, fmt.Errorf("%s: expected a single JSON object", path)
		}
	}
	if config == nil {
		return Config{}, fmt.Errorf("%s: expected a JSON object", path)
	}
	if config.StatusLine == nil {
		return Config{}, fmt.Errorf("%s: status_line must be a boolean", path)
	}
	var colors []string
	for i, setting := range []struct {
		name  string
		value *string
	}{
		{"border_color", config.BorderColor},
		{"status_active_tab_color", config.StatusActiveTabColor},
		{"status_active_tab_bg", config.StatusActiveTabBg},
	} {
		if setting.value == nil {
			return Config{}, fmt.Errorf("%s: %s must be a color string", path, setting.name)
		}
		if i > 0 && *setting.value == "" {
			continue
		}
		color, err := colorful.Hex(*setting.value)
		if err != nil {
			return Config{}, fmt.Errorf("%s: %s: %w", path, setting.name, err)
		}
		r, g, b := color.RGB255()
		code := 38
		if i == 2 {
			code = 48
		}
		colors = append(colors, fmt.Sprintf("%d;2;%d;%d;%d", code, r, g, b))
	}
	config.borderStyle = "\x1b[" + colors[0] + "m"
	if len(colors) > 1 {
		config.activeTabStyle = "\x1b[" + strings.Join(colors[1:], ";") + "m"
	}
	names := []string{"status_line_left", "status_line_center", "status_line_right"}
	texts := []*string{config.StatusLineLeft, config.StatusLineCenter, config.StatusLineRight}
	for i, name := range names {
		if texts[i] == nil {
			return Config{}, fmt.Errorf("%s: %s must be a string", path, name)
		}
		t, err := template.New(name).Option("missingkey=error").Funcs(template.FuncMap{
			"highlight": func(text string) string { return config.activeTabStyle + cleanText(text) + "\x1b[39;49m" },
		}).Parse(*texts[i])
		if err != nil {
			return Config{}, fmt.Errorf("%s: %w", path, err)
		}
		config.statusTemplates[i] = t
	}
	return *config, nil
}
