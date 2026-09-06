GOGIO_VERSION := v0.10.0
APP_ID := org.lichess.engine-host
OAUTH_SCHEME := org.lichess.engine-host
MACOS_ARCH ?= $(shell go env GOARCH)
MACOS_VERSION ?= 1.0.0.1
MACOS_APP := build/Native Engine Host.app
MACOS_SIGN_KEY ?=
MACOS_NOTARY_ID ?=
MACOS_NOTARY_TEAM_ID ?=

.PHONY: macos clean

macos:
	mkdir -p build
	go run gioui.org/cmd/gogio@$(GOGIO_VERSION) \
		-target macos \
		-arch "$(MACOS_ARCH)" \
		-appid "$(APP_ID)" \
		-name "Native Engine Host" \
		-version "$(MACOS_VERSION)" \
		-schemes "$(OAUTH_SCHEME)" \
		-o "$(MACOS_APP)" \
		$(if $(MACOS_SIGN_KEY),-signkey "$(MACOS_SIGN_KEY)") \
		$(if $(MACOS_NOTARY_ID),-notaryid "$(MACOS_NOTARY_ID)") \
		$(if $(MACOS_NOTARY_TEAM_ID),-notaryteamid "$(MACOS_NOTARY_TEAM_ID)") \
		.

clean:
	rm -rf build
