GOGIO_VERSION := v0.10.0
APP_ID := org.lichess.direct-engine-host
MACOS_ARCH ?= $(shell go env GOARCH)
MACOS_VERSION ?= 1.0.0.1
MACOS_APP := build/Direct Engine Host.app
MACOS_SIGN_KEY ?=
MACOS_NOTARY_ID ?=
MACOS_NOTARY_TEAM_ID ?=
PREFIX ?= $(HOME)/.local

.PHONY: linux install-linux macos clean

linux:
	mkdir -p build
	go build -ldflags "-X gioui.org/app.ID=$(APP_ID)" -o build/direct-engine-host .

install-linux: linux
	install -Dm755 build/direct-engine-host "$(PREFIX)/bin/direct-engine-host"
	install -Dm644 packaging/linux/$(APP_ID).desktop "$(PREFIX)/share/applications/$(APP_ID).desktop"
	desktop-file-edit --set-key=Exec --set-value='"$(PREFIX)/bin/direct-engine-host"' "$(PREFIX)/share/applications/$(APP_ID).desktop"

macos:
	mkdir -p build
	go run gioui.org/cmd/gogio@$(GOGIO_VERSION) \
		-target macos \
		-arch "$(MACOS_ARCH)" \
		-appid "$(APP_ID)" \
		-name "Direct Engine Host" \
		-version "$(MACOS_VERSION)" \
		-o "$(MACOS_APP)" \
		$(if $(MACOS_SIGN_KEY),-signkey "$(MACOS_SIGN_KEY)") \
		$(if $(MACOS_NOTARY_ID),-notaryid "$(MACOS_NOTARY_ID)") \
		$(if $(MACOS_NOTARY_TEAM_ID),-notaryteamid "$(MACOS_NOTARY_TEAM_ID)") \
		.

clean:
	rm -rf build
