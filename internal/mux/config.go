package mux

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

type Config struct {
	StatusLine bool
}

func loadConfig(home string) (Config, error) {
	config := Config{StatusLine: true}
	path := filepath.Join(home, ".bs", "config.json")
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return config, nil
	}
	if err != nil {
		return config, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, 65537))
	if err != nil {
		return config, err
	}
	if len(data) > 65536 {
		return config, fmt.Errorf("%s: configuration exceeds 64 KiB", path)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return config, fmt.Errorf("%s: %w", path, err)
	}
	if fields == nil {
		return config, fmt.Errorf("%s: expected a JSON object", path)
	}
	for key, value := range fields {
		if key != "status_line" {
			return config, fmt.Errorf("%s: unknown setting %q", path, key)
		}
		value = bytes.TrimSpace(value)
		if !bytes.Equal(value, []byte("true")) && !bytes.Equal(value, []byte("false")) {
			return config, fmt.Errorf("%s: status_line must be a boolean", path)
		}
		config.StatusLine = bytes.Equal(value, []byte("true"))
	}
	return config, nil
}
