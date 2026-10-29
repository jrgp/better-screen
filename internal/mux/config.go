package mux

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"text/template"
)

type Config struct {
	StatusLine       *bool   `json:"status_line"`
	StatusLineLeft   *string `json:"status_line_left"`
	StatusLineCenter *string `json:"status_line_center"`
	StatusLineRight  *string `json:"status_line_right"`
	statusTemplates  [3]*template.Template
}

const defaultStatusLeft = `{{ range .Windows }}{{ if .Active }}{{ highlight (printf "%d:%s" .Number .Title) }}{{ else }}{{ .Number }}:{{ .Title }}{{ end }} {{ end }}`

func loadConfig(home string) (Config, error) {
	// Omitted fields keep their defaults; explicit null values become nil.
	config := &Config{
		StatusLine:       new(true),
		StatusLineLeft:   new(defaultStatusLeft),
		StatusLineCenter: new(""),
		StatusLineRight:  new("{{ .Session.Name }}"),
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
	names := []string{"status_line_left", "status_line_center", "status_line_right"}
	texts := []*string{config.StatusLineLeft, config.StatusLineCenter, config.StatusLineRight}
	for i, name := range names {
		if texts[i] == nil {
			return Config{}, fmt.Errorf("%s: %s must be a string", path, name)
		}
		t, err := template.New(name).Option("missingkey=error").Funcs(template.FuncMap{
			"highlight": func(text string) string { return accent + cleanText(text) + "\x1b[39m" },
		}).Parse(*texts[i])
		if err != nil {
			return Config{}, fmt.Errorf("%s: %w", path, err)
		}
		config.statusTemplates[i] = t
	}
	return *config, nil
}
