GOPATH := $(shell go env GOPATH)
GOARCH ?= $(shell go env GOARCH)
GOOS ?= $(shell go env GOOS)
NPM ?= npm
NPM_DIST_TAG ?= dev
GLOBAL_TYPES_DIR := global-types


ifeq ($(GOOS),windows)
GO_SERVER_NAME := gitea-pages.exe
GO_LOCAL_NAME := local-server.exe
else
GO_SERVER_NAME := gitea-pages
GO_LOCAL_NAME := local-server
endif

fmt:
	@(test -f "$(GOPATH)/bin/gofumpt" || go install mvdan.cc/gofumpt@latest) && \
	"$(GOPATH)/bin/gofumpt" -l -w .


.PHONY: release
release: dist/gitea-pages-$(GOOS)-$(GOARCH).tar.gz

dist/gitea-pages-$(GOOS)-$(GOARCH).tar.gz:  $(shell find . -type f -name "*.go" ) go.mod go.sum
	@echo Compile $@ via $(GO_SERVER_NAME) && \
	mkdir -p dist && \
	rm -f dist/$(GO_SERVER_NAME) && \
	GOOS=$(GOOS) GOARCH=$(GOARCH) CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o dist/$(GO_SERVER_NAME) ./cmd/server && \
	GOOS=$(GOOS) GOARCH=$(GOARCH) CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o dist/$(GO_LOCAL_NAME) ./cmd/local && \
	cd dist && \
	tar zcf gitea-pages-$(GOOS)-$(GOARCH).tar.gz $(GO_SERVER_NAME) $(GO_LOCAL_NAME) ../LICENSE ../config.yaml ../cmd/server/errors.html.tmpl ../README.md ../README_*.md && \
	rm -f $(GO_SERVER_NAME) $(GO_LOCAL_NAME)

gitea-pages: $(shell find . -type f -name "*.go" ) go.mod go.sum
	@CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o $@ ./cmd/server

.PHONY: debug
debug: gitea-pages
	@./gitea-pages -conf config-local.yaml -debug

.PHONY: test
test:
	@go test -v -coverprofile=coverage.txt ./...


.PHONY: releases
releases:
	@make release GOOS=linux GOARCH=amd64 && \
	make release GOOS=linux GOARCH=arm64 && \
	make release GOOS=linux GOARCH=loong64 && \
	make release GOOS=windows GOARCH=amd64

.PHONY: lint
lint:
	@(test -f "$(GOPATH)/bin/golangci-lint" || go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.6.0) && \
	"$(GOPATH)/bin/golangci-lint" run -c .golangci.yml

.PHONY: lint-fix
lint-fix:
	@(test -f "$(GOPATH)/bin/golangci-lint" || go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.6.0) && \
	"$(GOPATH)/bin/golangci-lint" run -c .golangci.yml --fix

.PHONY: push
push:
	@set -eu; \
	if [ -n "$$(git status --porcelain)" ]; then \
		echo "refuse to publish: git worktree is not clean" >&2; \
		exit 1; \
	fi; \
	exact_tag="$$(git describe --tags --exact-match HEAD 2>/dev/null || true)"; \
	if [ -n "$$exact_tag" ]; then \
		version="$${exact_tag#v}"; \
	else \
		base_tag="$$(git describe --tags --abbrev=0 HEAD 2>/dev/null || true)"; \
		if [ -z "$$base_tag" ]; then \
			echo "refuse to publish: no reachable git tag found for HEAD" >&2; \
			exit 1; \
		fi; \
		commit_count="$$(git rev-list --count "$$base_tag"..HEAD)"; \
		short_sha="$$(git rev-parse --short HEAD)"; \
		version="$${base_tag#v}-dev.$$commit_count.$$short_sha"; \
	fi; \
	tmp_dir="$$(mktemp -d)"; \
	trap 'rm -rf "$$tmp_dir"' EXIT INT TERM HUP; \
	cp -R "$(GLOBAL_TYPES_DIR)"/. "$$tmp_dir"/; \
	echo "publishing $(GLOBAL_TYPES_DIR) version $$version with dist-tag $(NPM_DIST_TAG)"; \
	cd "$$tmp_dir" && \
		node -e 'const fs=require("fs"); const version=process.argv[1]; for (const file of ["package.json","package-lock.json"]) { if (!fs.existsSync(file)) continue; const data=JSON.parse(fs.readFileSync(file,"utf8")); data.version=version; if (data.packages && data.packages[""]) data.packages[""].version=version; fs.writeFileSync(file, JSON.stringify(data, null, 2)+"\n"); }' "$$version" && \
		"$(NPM)" publish --tag "$(NPM_DIST_TAG)" --access public

EXAMPLE_DIRS := $(shell find examples -maxdepth 1 -type d ! -path "examples" | sort)
.PHONY: $(EXAMPLE_DIRS)
$(EXAMPLE_DIRS):
	@go run ./cmd/local/main.go -path $@
