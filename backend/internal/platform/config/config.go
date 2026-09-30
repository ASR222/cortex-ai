// Package config reads service configuration from environment variables.
//
// Every service calls these helpers once at startup. Missing required values
// are collected and reported together so a misconfigured deployment fails fast
// with one clear error instead of crashing later on the first request.
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Loader accumulates errors while reading variables.
type Loader struct {
	missing []string
	invalid []string
}

// Required returns the value of key or records it as missing.
func (l *Loader) Required(key string) string {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		l.missing = append(l.missing, key)
	}
	return v
}

// String returns the value of key, or def when unset.
func (l *Loader) String(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}

// Int returns key parsed as an int, or def when unset.
func (l *Loader) Int(key string, def int) int {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		l.invalid = append(l.invalid, key)
		return def
	}
	return n
}

// Bool returns key parsed as a bool, or def when unset.
func (l *Loader) Bool(key string, def bool) bool {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return def
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		l.invalid = append(l.invalid, key)
		return def
	}
	return b
}

// Duration returns key parsed with time.ParseDuration, or def when unset.
func (l *Loader) Duration(key string, def time.Duration) time.Duration {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return def
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		l.invalid = append(l.invalid, key)
		return def
	}
	return d
}

// Err reports every missing or invalid variable, or nil.
func (l *Loader) Err() error {
	if len(l.missing) == 0 && len(l.invalid) == 0 {
		return nil
	}
	var parts []string
	if len(l.missing) > 0 {
		parts = append(parts, "missing: "+strings.Join(l.missing, ", "))
	}
	if len(l.invalid) > 0 {
		parts = append(parts, "invalid: "+strings.Join(l.invalid, ", "))
	}
	return fmt.Errorf("config: %s", strings.Join(parts, "; "))
}

// Addr returns the listen address: ADDR if set, else ":$PORT" (Cloud Run
// injects PORT), else def.
func (l *Loader) Addr(def string) string {
	if v := strings.TrimSpace(os.Getenv("ADDR")); v != "" {
		return v
	}
	if p := strings.TrimSpace(os.Getenv("PORT")); p != "" {
		return ":" + p
	}
	return def
}
