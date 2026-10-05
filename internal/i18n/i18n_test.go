package i18n

import "testing"

func TestResolveAndTranslate(t *testing.T) {
	defer SetPreference("")
	defer SetSystem("")
	SetSystem("en_US.UTF-8")
	SetPreference("")
	if Current() != English || T("Quit") != "Quit" {
		t.Fatalf("system English: %s %q", Current(), T("Quit"))
	}
	SetSystem("zh_CN.UTF-8")
	if Current() != Chinese || T("Quit") != "退出" {
		t.Fatalf("system Chinese: %s %q", Current(), T("Quit"))
	}
	SetPreference("en")
	if Current() != English {
		t.Fatal("the preference wins over the system")
	}
	SetPreference("zh-CN")
	if got := T("%d new messages from %s", 3, "CI"); got != "CI 发来 3 条新消息" {
		t.Fatalf("reordered arguments: %q", got)
	}
	if got := T("Not translated %d", 1); got != "Not translated 1" {
		t.Fatalf("fallback: %q", got)
	}
}

// Every translation must take the same arguments as its English text.
func TestTranslationsKeepTheirVerbs(t *testing.T) {
	count := func(s string) int {
		n := 0
		for i := 0; i < len(s)-1; i++ {
			if s[i] == '%' && s[i+1] != '%' {
				n++
			}
		}
		return n
	}
	for en, zh := range chinese {
		if count(en) != count(zh) {
			t.Errorf("%q has %d verbs, %q has %d", en, count(en), zh, count(zh))
		}
	}
}
