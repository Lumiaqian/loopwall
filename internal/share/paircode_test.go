package share

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
)

func testFingerprint(seed string) string {
	sum := sha256.Sum256([]byte(seed))
	return hex.EncodeToString(sum[:])
}

func TestPairCodeShape(t *testing.T) {
	code, err := EncodePairCode(testFingerprint("a"), "042917")
	if err != nil {
		t.Fatal(err)
	}
	groups := strings.Split(code, "-")
	if len(groups) != 8 {
		t.Fatalf("%q has %d groups, want 8", code, len(groups))
	}
	for _, g := range groups {
		if len(g) != 4 {
			t.Fatalf("group %q of %q is not 4 characters", g, code)
		}
	}
	if code != strings.ToUpper(code) {
		t.Errorf("%q is not upper case", code)
	}
}

func TestPairCodeRoundTrip(t *testing.T) {
	for _, secret := range []string{"000000", "000001", "042917", "524288", "999999"} {
		for _, seed := range []string{"a", "b", "loopwall"} {
			fp := testFingerprint(seed)
			code, err := EncodePairCode(fp, secret)
			if err != nil {
				t.Fatal(err)
			}
			got, err := ParsePairCode(code)
			if err != nil {
				t.Fatalf("%s: %v", code, err)
			}
			if got.Secret != secret || !got.Matches(fp) || got.Matches(testFingerprint("other")) {
				t.Errorf("%s: parsed %+v, want secret %s of %s", code, got, secret, fp)
			}
		}
	}
}

// TestPairCodeParsesWhatIsTyped checks the forgiving reading: case, dashes,
// spaces, and the characters base32 has no digit for.
func TestPairCodeParsesWhatIsTyped(t *testing.T) {
	fp := testFingerprint("typed")
	code, _ := EncodePairCode(fp, "123456")
	want, err := ParsePairCode(code)
	if err != nil {
		t.Fatal(err)
	}
	for _, typed := range []string{
		strings.ToLower(code),
		strings.ReplaceAll(code, "-", ""),
		strings.ReplaceAll(code, "-", " "),
		"  " + code + "\n",
		strings.ReplaceAll(strings.ReplaceAll(code, "-", " "), " ", "  "),
	} {
		got, err := ParsePairCode(typed)
		if err != nil || got != want {
			t.Errorf("%q: %+v, %v", typed, got, err)
		}
	}
	// 0, 1 and 8 are read as O, I and B: a code with those letters
	// parses when typed with the digits.
	for seed := 0; seed < 200; seed++ {
		c, _ := EncodePairCode(testFingerprint(string(rune('a'+seed%26))+strings.Repeat("x", seed)), "000000")
		if !strings.ContainsAny(c, "OIB") {
			continue
		}
		digits := strings.NewReplacer("O", "0", "I", "1", "B", "8").Replace(c)
		a, errA := ParsePairCode(c)
		b, errB := ParsePairCode(digits)
		if errA != nil || errB != nil || a != b {
			t.Fatalf("%s vs %s: %v %v", c, digits, errA, errB)
		}
		return
	}
	t.Fatal("no sample code with O, I or B")
}

// TestPairCodeEverySingleTypoIsCaught checks, for every position and every
// other character of the alphabet, that the check characters notice.
func TestPairCodeEverySingleTypoIsCaught(t *testing.T) {
	code, _ := EncodePairCode(testFingerprint("typo"), "314159")
	plain := strings.ReplaceAll(code, "-", "")
	if len(plain) != codeLen {
		t.Fatal(len(plain))
	}
	for i := range plain {
		for _, r := range codeAlphabet {
			if byte(r) == plain[i] {
				continue
			}
			typo := plain[:i] + string(r) + plain[i+1:]
			if _, err := ParsePairCode(typo); !errors.Is(err, ErrCodeTypo) {
				t.Fatalf("position %d, %c for %c: %v, want ErrCodeTypo", i, r, plain[i], err)
			}
		}
	}
}

func TestPairCodeTypoErrors(t *testing.T) {
	code, _ := EncodePairCode(testFingerprint("typo"), "314159")
	plain := []byte(strings.ReplaceAll(code, "-", ""))
	plain[3] = 'A' + (plain[3]-'A'+1)%26
	if _, err := ParsePairCode(string(plain)); !errors.Is(err, ErrCodeTypo) {
		t.Errorf("a substituted character: %v, want ErrCodeTypo", err)
	}
	// A character outside the alphabet is a typo too.
	plain = []byte(strings.ReplaceAll(code, "-", ""))
	plain[5] = '9'
	if _, err := ParsePairCode(string(plain)); !errors.Is(err, ErrCodeTypo) {
		t.Errorf("a 9: %v, want ErrCodeTypo", err)
	}
	// Swapping two neighbours is usually caught as well.
	plain = []byte(strings.ReplaceAll(code, "-", ""))
	for i := 0; i+1 < codeHalf; i++ {
		if plain[i] == plain[i+1] {
			continue
		}
		swapped := append([]byte(nil), plain...)
		swapped[i], swapped[i+1] = swapped[i+1], swapped[i]
		if _, err := ParsePairCode(string(swapped)); err == nil {
			t.Logf("swap at %d not caught (Luhn mod N misses some)", i)
		}
	}
}

func TestPairCodeWrongLength(t *testing.T) {
	code, _ := EncodePairCode(testFingerprint("len"), "000001")
	plain := strings.ReplaceAll(code, "-", "")
	for _, s := range []string{"", "ABCD", plain[:31], plain + "A", plain + plain, "12345", "1234567"} {
		if _, err := ParsePairCode(s); !errors.Is(err, ErrCodeFormat) {
			t.Errorf("%q: %v, want ErrCodeFormat", s, err)
		}
	}
}

// TestPairCodeRejectsWhatItNeverMakes checks a secret over 999999 and
// padding bits that are not zero, which only a forged code has, even with
// right check characters.
func TestPairCodeRejectsWhatItNeverMakes(t *testing.T) {
	forge := func(raw []byte) string {
		chars := codeEncoding.EncodeToString(raw)[:codePayloadChars]
		return chars[:codeHalf] + string(luhn32(chars[:codeHalf])) + chars[codeHalf:] + string(luhn32(chars[codeHalf:]))
	}
	head, _ := hex.DecodeString(testFingerprint("forged")[:32])
	big := (1_000_000) << 4 // the first secret that is not six digits
	if _, err := ParsePairCode(forge(append(append([]byte(nil), head...), byte(big>>16), byte(big>>8), byte(big)))); !errors.Is(err, ErrCodeFormat) {
		t.Errorf("secret 1000000: %v", err)
	}
	if _, err := ParsePairCode(forge(append(append([]byte(nil), head...), 0, 0, 0x08))); !errors.Is(err, ErrCodeFormat) {
		t.Errorf("padding bit set: %v", err)
	}
	ok := 999_999 << 4
	if got, err := ParsePairCode(forge(append(append([]byte(nil), head...), byte(ok>>16), byte(ok>>8), byte(ok)))); err != nil || got.Secret != "999999" {
		t.Errorf("secret 999999: %+v %v", got, err)
	}
}

func TestLegacyCode(t *testing.T) {
	for s, want := range map[string]bool{"123456": true, "123 456": true, " 000000 ": true, "12345": false, "1234567": false, "12345a": false, "": false} {
		if IsLegacyCode(s) != want {
			t.Errorf("IsLegacyCode(%q) = %v", s, !want)
		}
	}
	if LegacySecret(" 123 456 ") != "123456" {
		t.Error("LegacySecret")
	}
	// Digits that are legacy are not mapped to letters: the long code has
	// 32 characters, so none of them is one.
	code, _ := EncodePairCode(testFingerprint("x"), "123456")
	if IsLegacyCode(code) {
		t.Error("a long code is legacy")
	}
}

func TestEncodePairCodeRejectsBadInput(t *testing.T) {
	if _, err := EncodePairCode("abcd", "123456"); err == nil {
		t.Error("short fingerprint")
	}
	if _, err := EncodePairCode(strings.Repeat("z", 64), "123456"); err == nil {
		t.Error("non-hex fingerprint")
	}
	if _, err := EncodePairCode(testFingerprint("x"), "12345"); err == nil {
		t.Error("short secret")
	}
}
