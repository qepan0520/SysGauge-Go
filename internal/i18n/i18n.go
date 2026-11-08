// Package i18n holds UI strings for every supported language.
//
// The string tables live in strings.go, generated from the Python edition so
// both versions speak exactly the same wording.
package i18n

import (
	"regexp"
	"strings"
	"sync"

	"golang.org/x/sys/windows"
)

var procGetUserDefaultUILanguage = windows.NewLazySystemDLL("kernel32.dll").NewProc("GetUserDefaultUILanguage")

// DetectSystemLanguage maps the Windows UI language to a supported code.
func DetectSystemLanguage() string {
	r, _, _ := procGetUserDefaultUILanguage.Call()
	if r == 0 {
		return DefaultLanguage
	}
	primary := uint16(r) & 0x3FF
	switch primary {
	case 0x04: // LANG_CHINESE
		return "zh_CN"
	case 0x11: // LANG_JAPANESE
		return "ja"
	case 0x07: // LANG_GERMAN
		return "de"
	case 0x0A: // LANG_SPANISH
		return "es"
	}
	return DefaultLanguage
}

var (
	mu      sync.RWMutex
	current = DefaultLanguage
)

// Normalize maps an arbitrary locale string to a supported language code.
func Normalize(code string) string {
	if code == "" {
		return DefaultLanguage
	}
	code = strings.ReplaceAll(code, "-", "_")
	if _, ok := translations[code]; ok {
		return code
	}
	short := strings.ToLower(strings.SplitN(code, "_", 2)[0])
	for candidate := range translations {
		if strings.ToLower(strings.SplitN(candidate, "_", 2)[0]) == short {
			return candidate
		}
	}
	return DefaultLanguage
}

// SetLanguage switches the active language and returns the normalized code.
func SetLanguage(code string) string {
	code = Normalize(code)
	mu.Lock()
	current = code
	mu.Unlock()
	return code
}

// Current returns the active language code.
func Current() string {
	mu.RLock()
	defer mu.RUnlock()
	return current
}

// Name returns the display name of a language code.
func Name(code string) string {
	code = Normalize(code)
	for _, lang := range Languages {
		if lang.Code == code {
			return lang.Name
		}
	}
	return Languages[0].Name
}

// Raw returns the unformatted template for a key.
func Raw(key string) string {
	mu.RLock()
	code := current
	mu.RUnlock()
	if table, ok := translations[code]; ok {
		if v, ok := table[key]; ok {
			return v
		}
	}
	if v, ok := translations[DefaultLanguage][key]; ok {
		return v
	}
	return key
}

var placeholder = regexp.MustCompile(`\{(\w+)(?::[^}]*)?\}`)

// Tr renders a template and replaces "{name}" / "{name:.1f}" placeholders.
// Unknown placeholders are left untouched so a missing argument never panics.
func Tr(key string, args ...string) string {
	text := Raw(key)
	if len(args) == 0 {
		return text
	}
	values := make(map[string]string, len(args)/2)
	for i := 0; i+1 < len(args); i += 2 {
		values[args[i]] = args[i+1]
	}
	return placeholder.ReplaceAllStringFunc(text, func(match string) string {
		sub := placeholder.FindStringSubmatch(match)
		if v, ok := values[sub[1]]; ok {
			return v
		}
		return match
	})
}
