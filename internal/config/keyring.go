package config

import (
	"errors"

	"github.com/zalando/go-keyring"
)

const serviceName = "woffux"

func SetPassword(email, password string) error {
	return keyring.Set(serviceName, email, password)
}

func GetPassword(email string) (string, error) {
	return keyring.Get(serviceName, email)
}

// DeletePassword removes the password from the keychain; one that isn't
// there is not an error.
func DeletePassword(email string) error {
	if err := keyring.Delete(serviceName, email); err != nil && !errors.Is(err, keyring.ErrNotFound) {
		return err
	}
	return nil
}
