package i18n

import (
	"strings"
	"testing"
)

func TestNormalize(t *testing.T) {
	cases := map[string]string{
		"zh-CN": "zh_CN",
		"zh_CN": "zh_CN",
		"ZH_cn": "zh_CN",
		"ja-JP": "ja",
		"de":    "de",
		"es-ES": "es",
		"fr-FR": "en",
		"":      "en",
		"en-US": "en",
	}
	for input, want := range cases {
		if got := Normalize(input); got != want {
			t.Fatalf("Normalize(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestTranslationsAreComplete(t *testing.T) {
	english := translations[DefaultLanguage]
	for _, lang := range Languages {
		table, ok := translations[lang.Code]
		if !ok {
			t.Fatalf("missing table for %s", lang.Code)
		}
		for key := range english {
			if _, ok := table[key]; !ok {
				t.Fatalf("%s is missing key %q", lang.Code, key)
			}
		}
		for key := range table {
			if _, ok := english[key]; !ok {
				t.Fatalf("%s has extra key %q", lang.Code, key)
			}
		}
	}
}

func TestTrFillsPlaceholders(t *testing.T) {
	SetLanguage("en")
	defer SetLanguage(DefaultLanguage)

	got := Tr("window.title", "app", "SysGauge", "version", "9.9")
	if !strings.Contains(got, "SysGauge") || !strings.Contains(got, "9.9") {
		t.Fatalf("placeholders were not filled: %q", got)
	}

	partial := Tr("notify.stopped.body", "n", "3")
	if !strings.Contains(partial, "3") || !strings.Contains(partial, "{name}") {
		t.Fatalf("missing argument must be left untouched: %q", partial)
	}

	if got := Raw("does.not.exist"); got != "does.not.exist" {
		t.Fatalf("unknown key = %q", got)
	}
}

func TestName(t *testing.T) {
	if Name("zh-CN") != "简体中文" {
		t.Fatalf("Name = %q", Name("zh-CN"))
	}
	if Name("unknown") != "English" {
		t.Fatalf("Name fallback = %q", Name("unknown"))
	}
}
