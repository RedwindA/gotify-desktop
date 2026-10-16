// Licensed under CC0-1.0 Universial by https://github.com/rocicorp/fracdex
package fracdex

import "testing"

func TestKeys(t *testing.T) {
	test := func(a, b, exp string) {
		t.Helper()
		act, err := KeyBetween(a, b)
		if err != nil {
			act = err.Error()
		}
		if act != exp {
			t.Errorf("KeyBetween(%q, %q) = %q, want %q", a, b, act, exp)
		}
	}

	test("", "", "a0")
	test("", "a0", "Zz")
	test("", "Zz", "Zy")
	test("a0", "", "a1")
	test("a1", "", "a2")
	test("a0", "a1", "a0V")
	test("a1", "a2", "a1V")
	test("a0V", "a1", "a0l")
	test("Zz", "a0", "ZzV")
	test("Zz", "a1", "a0")
	test("", "Y00", "Xzzz")
	test("bzz", "", "c000")
	test("a0", "a0V", "a0G")
	test("a0", "a0G", "a08")
	test("b125", "b129", "b127")
	test("a0", "a1V", "a1")
	test("Zz", "a01", "a0")
	test("", "a0V", "a0")
	test("", "b999", "b99")
	test("aV", "aV0V", "aV0G")
	test(
		"",
		"A00000000000000000000000000",
		"invalid order key: A00000000000000000000000000",
	)
	test("", "A000000000000000000000000001", "A000000000000000000000000000V")
	test("zzzzzzzzzzzzzzzzzzzzzzzzzzy", "", "zzzzzzzzzzzzzzzzzzzzzzzzzzz")
	test("zzzzzzzzzzzzzzzzzzzzzzzzzzz", "", "zzzzzzzzzzzzzzzzzzzzzzzzzzzV")
	test("a00", "", "invalid order key: a00")
	test("a00", "a1", "invalid order key: a00")
	test("0", "1", "invalid order key head: 0")
	test("a1", "a0", "a1 >= a0")
}
