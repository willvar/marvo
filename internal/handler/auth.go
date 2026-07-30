package handler

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"marvo/config"

	"github.com/gofiber/fiber/v2"
)

type AuthHandler struct {
	config *config.Config
}

func (d *Dependencies) Verify(c *fiber.Ctx) error {
	var body struct {
		Password string `json:"password"`
	}
	if err := c.BodyParser(&body); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "invalid request"})
	}

	if body.Password != d.Config.Auth.Password {
		return c.Status(401).JSON(fiber.Map{"error": "invalid password"})
	}

	challenge := generateChallenge()
	expiry := time.Now().Add(5 * time.Minute).Unix()

	payload := fmt.Sprintf("%s:%d", challenge, expiry)
	sig := signPayload(payload, d.Config.Server.SessionSecret)

	token := fmt.Sprintf("%s:%d:%s", challenge, expiry, sig)
	return c.JSON(fiber.Map{"challenge_token": token})
}

func (d *Dependencies) Login(c *fiber.Ctx) error {
	var body struct {
		ChallengeToken string `json:"challenge_token"`
	}
	if err := c.BodyParser(&body); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "invalid request"})
	}

	challenge, expiryStr, sig, err := parseChallengeToken(body.ChallengeToken)
	if err != nil {
		return c.Status(401).JSON(fiber.Map{"error": "invalid challenge token"})
	}

	expiry, err := strconv.ParseInt(expiryStr, 10, 64)
	if err != nil || time.Now().Unix() > expiry {
		return c.Status(401).JSON(fiber.Map{"error": "challenge token expired"})
	}

	expectedSig := signPayload(fmt.Sprintf("%s:%s", challenge, expiryStr), d.Config.Server.SessionSecret)
	if !hmac.Equal([]byte(sig), []byte(expectedSig)) {
		return c.Status(401).JSON(fiber.Map{"error": "invalid challenge token"})
	}

	sessionValue := fmt.Sprintf("marvo:%d:%s", time.Now().Unix(), challenge)
	sessionSig := signPayload(sessionValue, d.Config.Server.SessionSecret)
	sessionToken := fmt.Sprintf("%s:%s", sessionValue, sessionSig)

	c.Cookie(&fiber.Cookie{
		Name:     "marvo_session",
		Value:    sessionToken,
		Path:     "/",
		MaxAge:   86400 * 7,
		HTTPOnly: true,
		SameSite: "Lax",
	})

	return c.JSON(fiber.Map{"ok": true})
}

func (d *Dependencies) Logout(c *fiber.Ctx) error {
	c.Cookie(&fiber.Cookie{
		Name:     "marvo_session",
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HTTPOnly: true,
	})
	return c.JSON(fiber.Map{"ok": true})
}

func (d *Dependencies) AuthMiddleware() fiber.Handler {
	return func(c *fiber.Ctx) error {
		if isPublicPath(c.Path()) {
			return c.Next()
		}

		session := c.Cookies("marvo_session")
		if session == "" {
			return c.Status(401).JSON(fiber.Map{"error": "unauthorized"})
		}

		value, sig, found := cutLast(session, ":")
		if !found {
			return c.Status(401).JSON(fiber.Map{"error": "invalid session"})
		}

		expectedSig := signPayload(value, d.Config.Server.SessionSecret)
		if !hmac.Equal([]byte(sig), []byte(expectedSig)) {
			return c.Status(401).JSON(fiber.Map{"error": "invalid session"})
		}

		return c.Next()
	}
}

func isPublicPath(path string) bool {
	public := []string{"/api/auth/verify", "/api/auth", "/api/auth/logout"}
	for _, p := range public {
		if path == p {
			return true
		}
	}
	return false
}

func generateChallenge() string {
	b := make([]byte, 32)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func signPayload(payload string, secret string) string {
	if secret == "" {
		secret = "marvo-default-secret"
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(payload))
	return hex.EncodeToString(mac.Sum(nil))
}

func parseChallengeToken(token string) (challenge, expiry, sig string, err error) {
	parts := splitN(token, ":", 3)
	if len(parts) != 3 {
		return "", "", "", fmt.Errorf("invalid token format")
	}
	return parts[0], parts[1], parts[2], nil
}

func splitN(s string, sep string, n int) []string {
	var result []string
	remaining := s
	for i := 0; i < n-1 && len(remaining) > 0; i++ {
		idx := indexOf(remaining, sep)
		if idx < 0 {
			break
		}
		result = append(result, remaining[:idx])
		remaining = remaining[idx+len(sep):]
	}
	result = append(result, remaining)
	return result
}

func indexOf(s string, sep string) int {
	for i := 0; i <= len(s)-len(sep); i++ {
		if s[i:i+len(sep)] == sep {
			return i
		}
	}
	return -1
}

func cutLast(s string, sep string) (before string, after string, found bool) {
	for i := len(s) - 1; i >= 0; i-- {
		if i+len(sep) <= len(s) && s[i:i+len(sep)] == sep {
			return s[:i], s[i+len(sep):], true
		}
	}
	return s, "", false
}

func writeJSON(c *fiber.Ctx, status int, data interface{}) error {
	c.Status(status)
	return c.JSON(data)
}

func readJSONBody(c *fiber.Ctx, v interface{}) error {
	return json.Unmarshal(c.Body(), v)
}

func logAction(action string, details ...interface{}) {
	slog.Info(action, details...)
}
