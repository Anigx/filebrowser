package users

import (
	"crypto/rand"
	"encoding/base64"
	"errors"

	"golang.org/x/crypto/bcrypt"

	fberrors "github.com/filebrowser/filebrowser/v2/errors"
)

func NewPasswordAfterKeyRemoval(newPassword, retiredHash string, minimum uint) (string, error) {
	if newPassword == "" {
		return "", fberrors.ErrEmptyPassword
	}
	if CheckPwd(newPassword, retiredHash) {
		return "", errors.New("choose a different password from the one used before enrollment")
	}
	return ValidateAndHashPwd(newPassword, minimum)
}

// ValidateAndHashPwd validates and hashes a password.
func ValidateAndHashPwd(password string, minimumLength uint) (string, error) {
	if uint(len(password)) < minimumLength {
		return "", fberrors.ErrShortPassword{MinimumLength: minimumLength}
	}

	if _, ok := commonPasswords[password]; ok {
		return "", fberrors.ErrEasyPassword
	}

	return HashPwd(password)
}

// HashPwd hashes a password.
func HashPwd(password string) (string, error) {
	bytes, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	return string(bytes), err
}

// CheckPwd checks if a password is correct.
func CheckPwd(password, hash string) bool {
	err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
	return err == nil
}

func RandomPwd(passwordLength uint) (string, error) {
	randomPasswordBytes := make([]byte, passwordLength)
	var _, err = rand.Read(randomPasswordBytes)
	if err != nil {
		return "", err
	}

	// This is done purely to make the password human-readable
	var randomPasswordString = base64.URLEncoding.EncodeToString(randomPasswordBytes)
	return randomPasswordString, nil
}
