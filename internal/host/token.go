package host

import "runtime"

const tokenService = "org.lichess.engine-host"

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
