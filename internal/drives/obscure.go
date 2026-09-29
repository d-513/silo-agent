package drives

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
)

// rcloneKey is rclone's published obscure key (fs/config/obscure). Obscuring is
// not encryption: it only keeps passwords from being read at a glance, and
// rclone refuses plain passwords for options like webdav/smb `pass`.
var rcloneKey = []byte{
	0x9c, 0x93, 0x5b, 0x48, 0x73, 0x0a, 0x55, 0x4d,
	0x6b, 0xfd, 0x7c, 0x63, 0xc8, 0x86, 0xa9, 0x2b,
	0xd3, 0x90, 0x19, 0x8e, 0xb8, 0x12, 0x8a, 0xfb,
	0xf4, 0xde, 0x16, 0x2b, 0x8b, 0x95, 0xf6, 0x38,
}

// Obscure is `rclone obscure`: AES-256-CTR with a random IV prefix, base64url.
func Obscure(plain string) (string, error) {
	block, err := aes.NewCipher(rcloneKey)
	if err != nil {
		return "", err
	}
	out := make([]byte, aes.BlockSize+len(plain))
	iv := out[:aes.BlockSize]
	if _, err := rand.Read(iv); err != nil {
		return "", err
	}
	cipher.NewCTR(block, iv).XORKeyStream(out[aes.BlockSize:], []byte(plain))
	return base64.RawURLEncoding.EncodeToString(out), nil
}

// Reveal is `rclone reveal`.
func Reveal(obscured string) (string, error) {
	raw, err := base64.RawURLEncoding.DecodeString(obscured)
	if err != nil {
		return "", err
	}
	if len(raw) < aes.BlockSize {
		return "", errors.New("obscured value too short")
	}
	block, err := aes.NewCipher(rcloneKey)
	if err != nil {
		return "", err
	}
	out := make([]byte, len(raw)-aes.BlockSize)
	cipher.NewCTR(block, raw[:aes.BlockSize]).XORKeyStream(out, raw[aes.BlockSize:])
	return string(out), nil
}
