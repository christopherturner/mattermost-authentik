PLUGIN_ID ?= io.christopherturner.mattermost-authentik
PLUGIN_VERSION ?= $(shell node -p "require('./plugin.json').version")
BUNDLE_NAME ?= $(PLUGIN_ID)-$(PLUGIN_VERSION).tar.gz

GO ?= go
GOFLAGS ?= -trimpath -ldflags="-s -w"
CGO_ENABLED ?= 0

SERVER_PLATFORMS := linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64

.PHONY: all server webapp bundle clean test deploy

all: bundle

## Compile Go server plugin for all supported platforms
server:
	@echo "==> Building server binaries..."
	@mkdir -p server/dist
	@cd server && \
	for platform in $(SERVER_PLATFORMS); do \
		os=$$(echo $$platform | cut -d'/' -f1); \
		arch=$$(echo $$platform | cut -d'/' -f2); \
		ext=""; \
		if [ "$$os" = "windows" ]; then ext=".exe"; fi; \
		echo "    Compiling for $$os/$$arch..."; \
		CGO_ENABLED=$(CGO_ENABLED) GOOS=$$os GOARCH=$$arch \
			$(GO) build $(GOFLAGS) -o dist/plugin-$$os-$$arch$$ext .; \
	done
	@echo "==> Server build complete."

## Compile webapp bundle
webapp:
	@echo "==> Building webapp..."
	@cd webapp && npm install && npm run build
	@echo "==> Webapp build complete."

## Run server tests
test:
	@echo "==> Running server tests..."
	@cd server && $(GO) test -v ./...
	@echo "==> Tests passed."

## Package plugin into .tar.gz bundle for Mattermost upload
bundle: server webapp
	@echo "==> Packaging plugin bundle..."
	@rm -rf dist
	@mkdir -p dist/$(PLUGIN_ID)
	@cp plugin.json dist/$(PLUGIN_ID)/
	@cp LICENSE README.md dist/$(PLUGIN_ID)/ 2>/dev/null || true
	@mkdir -p dist/$(PLUGIN_ID)/server/dist
	@cp server/dist/* dist/$(PLUGIN_ID)/server/dist/
	@mkdir -p dist/$(PLUGIN_ID)/webapp/dist
	@cp webapp/dist/main.js dist/$(PLUGIN_ID)/webapp/dist/
	@if [ -d assets ]; then \
		mkdir -p dist/$(PLUGIN_ID)/assets; \
		cp -r assets/* dist/$(PLUGIN_ID)/assets/; \
	fi
	@cd dist && tar -czf $(BUNDLE_NAME) $(PLUGIN_ID)
	@echo "==> Plugin bundle created successfully:"
	@echo "    dist/$(BUNDLE_NAME)"

## Clean build artifacts
clean:
	@rm -rf dist server/dist webapp/dist webapp/node_modules
	@echo "==> Cleaned build directories."

## Optional direct upload to running Mattermost instance
deploy: bundle
	@if [ -z "$(MM_SERVICESETTINGS_SITEURL)" ] || [ -z "$(MM_ADMIN_TOKEN)" ]; then \
		echo "Error: MM_SERVICESETTINGS_SITEURL and MM_ADMIN_TOKEN must be set to deploy"; \
		exit 1; \
	fi
	@echo "==> Deploying $(BUNDLE_NAME) to $(MM_SERVICESETTINGS_SITEURL)..."
	@curl -s -f \
		-H "Authorization: Bearer $(MM_ADMIN_TOKEN)" \
		-F "plugin=@dist/$(BUNDLE_NAME)" \
		-F "force=true" \
		$(MM_SERVICESETTINGS_SITEURL)/api/v4/plugins
	@echo "\n==> Plugin deployed successfully."
