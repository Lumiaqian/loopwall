package share

import (
	"crypto/subtle"
	"encoding/base32"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// A pairing code, version 2, is what the sharing Mac shows and the other
// types: the first 16 bytes (128 bits) of the SHA-256 of the certificate
// its server presents, followed by its current one-time secret, the six
// digits of 0..999999, in 20 bits. That is 148 bits, 30 characters of
// RFC 4648 base32 once two zero bits pad it out. The 30 are cut into two
// halves of 15, each followed by a Luhn mod-32 check character, as
// Syncthing's device IDs are: 32 characters, shown as eight groups of
// four, "ABCD-EFGH-…". The fingerprint alone is public, as anyone may
// shake hands with the server for its certificate: the secret is what
// proves the user saw the sharing Mac's screen, and the fingerprint is
// what lets the typing Mac know whom it talks to before it sends that.

var (
	// ErrCodeTypo is a code whose check characters do not match: a
	// character was mistyped.
	ErrCodeTypo = errors.New("配对码有输错的字符，请核对")
	// ErrCodeFormat is a text that is not a pairing code at all.
	ErrCodeFormat = errors.New("配对码的长度或格式不对，请核对")
)

const (
	codeAlphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZ234567" // RFC 4648 base32
	// codeFingerprintLen is how many bytes of the certificate's
	// fingerprint a code carries.
	codeFingerprintLen = 16
	codePayloadChars   = 30
	codeHalf           = codePayloadChars / 2
	codeLen            = codePayloadChars + 2
	maxSecret          = 1_000_000
)

var codeEncoding = base32.StdEncoding.WithPadding(base32.NoPadding)

// PairCode is what a version 2 pairing code says.
type PairCode struct {
	Fingerprint [codeFingerprintLen]byte // start of the server certificate's SHA-256
	Secret      string                   // six digits
}

// Matches reports whether fingerprint, a certificate's SHA-256 in hex, is
// the one the code names.
func (c PairCode) Matches(fingerprint string) bool {
	return len(fingerprint) >= 2*codeFingerprintLen &&
		subtle.ConstantTimeCompare([]byte(fingerprint[:2*codeFingerprintLen]), []byte(hex.EncodeToString(c.Fingerprint[:]))) == 1
}

// EncodePairCode is the code for a server whose certificate has the
// fingerprint fp, in hex, and whose current secret is secret, six digits,
// as shown: groups of four, joined by dashes.
func EncodePairCode(fp, secret string) (string, error) {
	if len(fp) < 2*codeFingerprintLen {
		return "", errors.New("share: fingerprint too short")
	}
	head, err := hex.DecodeString(fp[:2*codeFingerprintLen])
	if err != nil {
		return "", err
	}
	if len(secret) != 6 || !isDigits(secret) {
		return "", errors.New("share: secret is not six digits")
	}
	n, _ := strconv.Atoi(secret)
	raw := make([]byte, 0, codeFingerprintLen+3)
	raw = append(raw, head...)
	n <<= 4 // 20 bits, then 4 zero bits to the byte
	raw = append(raw, byte(n>>16), byte(n>>8), byte(n))
	chars := codeEncoding.EncodeToString(raw)[:codePayloadChars] // the 31st is padding
	var b strings.Builder
	for _, half := range []string{chars[:codeHalf], chars[codeHalf:]} {
		b.WriteString(half)
		b.WriteByte(luhn32(half))
	}
	s := b.String()
	groups := make([]string, 0, codeLen/4)
	for i := 0; i < len(s); i += 4 {
		groups = append(groups, s[i:i+4])
	}
	return strings.Join(groups, "-"), nil
}

// ParsePairCode reads the code the user typed: in any case, with or
// without the dashes and spaces, 0 for O, 1 for I and 8 for B as
// Syncthing reads them. Its check characters are verified, so a mistyped
// character is ErrCodeTypo before anything touches the network.
func ParsePairCode(s string) (PairCode, error) {
	s = strings.ToUpper(strings.NewReplacer(" ", "", "-", "", "\t", "").Replace(strings.TrimSpace(s)))
	s = strings.NewReplacer("0", "O", "1", "I", "8", "B").Replace(s)
	if len(s) != codeLen {
		return PairCode{}, ErrCodeFormat
	}
	for _, half := range []string{s[:codeHalf+1], s[codeHalf+1:]} {
		want, ok := luhn32Check(half[:codeHalf])
		if !ok || want != half[codeHalf] {
			return PairCode{}, ErrCodeTypo
		}
	}
	raw, err := codeEncoding.DecodeString(s[:codeHalf] + s[codeHalf+1:codeLen-1] + "A")
	if err != nil || len(raw) != codeFingerprintLen+3 {
		return PairCode{}, ErrCodeFormat
	}
	n := int(raw[16])<<16 | int(raw[17])<<8 | int(raw[18])
	if n&0xf != 0 || n>>4 >= maxSecret {
		return PairCode{}, ErrCodeFormat
	}
	var code PairCode
	copy(code.Fingerprint[:], raw[:codeFingerprintLen])
	code.Secret = fmt.Sprintf("%06d", n>>4)
	return code, nil
}

// IsLegacyCode reports whether text is a code of versions before this
// one: six digits, alone.
func IsLegacyCode(text string) bool {
	text = strings.NewReplacer(" ", "", "-", "").Replace(strings.TrimSpace(text))
	return len(text) == 6 && isDigits(text)
}

// LegacySecret is the six digits of a legacy code typed with spaces.
func LegacySecret(text string) string {
	return strings.NewReplacer(" ", "", "-", "").Replace(strings.TrimSpace(text))
}

// luhn32 is the Luhn mod-32 check character of s, whose characters are
// of codeAlphabet.
func luhn32(s string) byte {
	c, _ := luhn32Check(s)
	return c
}

func luhn32Check(s string) (byte, bool) {
	const n = 32
	factor, sum := 1, 0
	for i := 0; i < len(s); i++ {
		cp := strings.IndexByte(codeAlphabet, s[i])
		if cp < 0 {
			return 0, false
		}
		addend := factor * cp
		factor = 3 - factor // 1, 2, 1, 2…
		sum += addend/n + addend%n
	}
	return codeAlphabet[(n-sum%n)%n], true
}

func isDigits(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return s != ""
}
