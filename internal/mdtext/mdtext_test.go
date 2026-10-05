package mdtext

import "testing"

func TestDecode(t *testing.T) {
	for in, want := range map[string]string{
		"Tom &amp; Jerry":                     "Tom & Jerry",
		`\*not italic\*`:                      "*not italic*",
		`\&amp; stays`:                        "&amp; stays",
		"&#35; &#x41; &lt;":                   "# A <",
		"a & b &unknownx;":                    "a & b &unknownx;",
		`back\slash`:                          `back\slash`,
		"plain":                               "plain",
		"&#039; &#065; &#x41; &#X61;":         "' A A a",
		"&#0; &#xD800; &#1114112; &#x110000;": "\uFFFD \uFFFD \uFFFD \uFFFD",
	} {
		if got := Decode([]byte(in)); got != want {
			t.Errorf("%q -> %q, want %q", in, got, want)
		}
	}
}
