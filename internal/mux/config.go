package mux

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"text/template"
)

type Config struct {
	StatusLine      bool
	statusTemplates [3]*template.Template
}

const defaultStatusLeft = `{{ range .Windows }}{{ if .Active }}{{ highlight (printf "%d:%s" .Number .Title) }}{{ else }}{{ .Number }}:{{ .Title }}{{ end }} {{ end }}`

func loadConfig(home string) (Config, error) {
	config := Config{StatusLine: true}
	names := []string{"status_line_left", "status_line_center", "status_line_right"}
	texts := []string{defaultStatusLeft, "", "{{ .Session.Name }}"}
	path := filepath.Join(home, ".bs", "config.json")
	f, err := os.Open(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return config, err
	}
	var fields map[string]json.RawMessage
	if err == nil {
		defer f.Close()
		data, err := io.ReadAll(io.LimitReader(f, 65537))
		if err != nil {
			return config, err
		}
		if len(data) > 65536 {
			return config, fmt.Errorf("%s: configuration exceeds 64 KiB", path)
		}
		if err := json.Unmarshal(data, &fields); err != nil {
			return config, fmt.Errorf("%s: %w", path, err)
		}
		if fields == nil {
			return config, fmt.Errorf("%s: expected a JSON object", path)
		}
	}
	for key, value := range fields {
		if key == "status_line" {
			value = bytes.TrimSpace(value)
			if !bytes.Equal(value, []byte("true")) && !bytes.Equal(value, []byte("false")) {
				return config, fmt.Errorf("%s: status_line must be a boolean", path)
			}
			config.StatusLine = bytes.Equal(value, []byte("true"))
			continue
		}
		i := slices.Index(names, key)
		if i < 0 {
			return config, fmt.Errorf("%s: unknown setting %q", path, key)
		}
		var text *string
		if err := json.Unmarshal(value, &text); err != nil || text == nil {
			return config, fmt.Errorf("%s: %s must be a string", path, key)
		}
		texts[i] = *text
	}
	for i, name := range names {
		t, err := template.New(name).Option("missingkey=error").Funcs(template.FuncMap{
			"highlight": func(text string) string { return accent + cleanText(text) + "\x1b[39m" },
		}).Parse(texts[i])
		if err != nil {
			return config, fmt.Errorf("%s: %w", path, err)
		}
		config.statusTemplates[i] = t
	}
	return config, nil
}
