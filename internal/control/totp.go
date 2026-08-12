package control

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base32"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	totpPeriod = int64(30)
	totpDigits = 6
)

func generateTOTPSecret() (string, error) {
	raw := make([]byte, 20)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(raw), nil
}

func (d *DB) encryptTOTPSecret(secret string) (string, error) {
	block, err := aes.NewCipher(d.totpKey[:])
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	sealed := gcm.Seal(nil, nonce, []byte(secret), []byte("marvo-user-totp-v1"))
	payload := append(nonce, sealed...)
	return "v1:" + base64.RawURLEncoding.EncodeToString(payload), nil
}

func (d *DB) decryptTOTPSecret(encoded string) (string, error) {
	version, payload, ok := strings.Cut(encoded, ":")
	if !ok || version != "v1" {
		return "", errors.New("unsupported TOTP secret format")
	}
	raw, err := base64.RawURLEncoding.DecodeString(payload)
	if err != nil {
		return "", errors.New("invalid TOTP secret")
	}
	block, err := aes.NewCipher(d.totpKey[:])
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	if len(raw) < gcm.NonceSize()+gcm.Overhead() {
		return "", errors.New("invalid TOTP secret")
	}
	plain, err := gcm.Open(nil, raw[:gcm.NonceSize()], raw[gcm.NonceSize():], []byte("marvo-user-totp-v1"))
	if err != nil {
		return "", errors.New("cannot decrypt TOTP secret")
	}
	return string(plain), nil
}

func totpCode(secret string, counter int64) (string, error) {
	key, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(strings.ToUpper(strings.TrimSpace(secret)))
	if err != nil || len(key) == 0 || counter < 0 {
		return "", errors.New("invalid TOTP input")
	}
	var message [8]byte
	binary.BigEndian.PutUint64(message[:], uint64(counter))
	mac := hmac.New(sha1.New, key)
	_, _ = mac.Write(message[:])
	sum := mac.Sum(nil)
	offset := sum[len(sum)-1] & 0x0f
	value := (uint32(sum[offset])&0x7f)<<24 |
		uint32(sum[offset+1])<<16 |
		uint32(sum[offset+2])<<8 |
		uint32(sum[offset+3])
	return fmt.Sprintf("%0*d", totpDigits, value%1_000_000), nil
}

func matchingTOTPCounter(secret, code string, now time.Time) (int64, bool) {
	code = strings.TrimSpace(code)
	if len(code) != totpDigits {
		return 0, false
	}
	if _, err := strconv.Atoi(code); err != nil {
		return 0, false
	}
	current := now.Unix() / totpPeriod
	for _, offset := range []int64{0, -1, 1} {
		counter := current + offset
		candidate, err := totpCode(secret, counter)
		if err == nil && hmac.Equal([]byte(candidate), []byte(code)) {
			return counter, true
		}
	}
	return 0, false
}

func (d *DB) totpURI(name, secret string) string {
	query := url.Values{}
	query.Set("secret", secret)
	query.Set("issuer", d.totpIssuer)
	query.Set("algorithm", "SHA1")
	query.Set("digits", strconv.Itoa(totpDigits))
	query.Set("period", strconv.FormatInt(totpPeriod, 10))
	return (&url.URL{
		Scheme:   "otpauth",
		Host:     "totp",
		Path:     "/" + d.totpIssuer + ":" + name,
		RawQuery: query.Encode(),
	}).String()
}
