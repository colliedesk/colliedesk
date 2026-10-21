package domain

import (
	"testing"

	"golang.org/x/text/unicode/norm"
)

func TestSlugifyKeepsTheWordsOfEveryScript(t *testing.T) {
	for name, want := range map[string]string{
		"Reset your password!":              "reset-your-password",
		"हिन्दी में सहायता":                 "हिन्दी-में-सहायता",
		"รีเซ็ตรหัสผ่าน":                    "รีเซ็ตรหัสผ่าน",
		norm.NFD.String("Đặt lại mật khẩu"): "đặt-lại-mật-khẩu",
		"Đặt lại mật khẩu":                  "đặt-lại-mật-khẩu",
		"如何重置密码":                            "如何重置密码",
		"  --  ":                            "fallback",
	} {
		got := Slugify(name, "fallback")
		if got != want {
			t.Errorf("Slugify(%q) = %q, want %q", name, got, want)
		}
		if got != "fallback" {
			if err := CheckKBSlug(got); err != nil {
				t.Errorf("CheckKBSlug(%q) refused the slug Slugify made: %v", got, err)
			}
		}
	}
	if err := CheckKBSlug("Not-A-Slug"); err == nil {
		t.Error("an uppercase slug was accepted")
	}
}
