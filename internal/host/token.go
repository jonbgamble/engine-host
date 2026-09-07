package host

import (
	"runtime"

	"github.com/zalando/go-keyring"
)

const tokenService = "org.lichess.engine-host"

type tokenStore interface {
	Get(account string) (string, error)
	Set(account, token string) error
}

type systemTokenStore struct{}

func (systemTokenStore) Get(account string) (string, error) {
	return keyring.Get(tokenService, account)
}

func (systemTokenStore) Set(account, token string) error {
	return keyring.Set(tokenService, account, token)
}

func tokenStoreName() string {
	switch runtime.GOOS {
	case "darwin":
		return "macOS Keychain"
	case "windows":
		return "Windows Credential Manager"
	case "linux":
		return "Secret Service"
	default:
		return "system credential storage"
	}
}
