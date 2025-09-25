GOPATH := $(shell go env GOPATH)
GOARCH ?= $(shell go env GOARCH)
GOOS ?= $(shell go env GOOS)


ifeq ($(GOOS),windows)
GO_DIST_NAME := gitea-pages.exe
else
GO_DIST_NAME := gitea-pages
endif

fmt:
	@(test -f "$(GOPATH)/bin/gofumpt" || go install mvdan.cc/gofumpt@latest) && \
	"$(GOPATH)/bin/gofumpt" -l -w .


.PHONY: release
release: dist/gitea-pages-$(GOOS)-$(GOARCH).tar.gz

dist/gitea-pages-$(GOOS)-$(GOARCH).tar.gz:  $(shell find . -type f -name "*.go" ) go.mod go.sum
	@echo Compile $@ via $(GO_DIST_NAME) && \
	mkdir -p dist && \
	rm -f dist/$(GO_DIST_NAME) && \
	GOOS=$(GOOS) GOARCH=$(GOARCH) CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o dist/$(GO_DIST_NAME) . && \
	cd dist && \
	tar zcf gitea-pages-$(GOOS)-$(GOARCH).tar.gz $(GO_DIST_NAME) ../LICENSE ../config.yaml ../errors.html.tmpl ../README.md ../README_*.md && \
	rm -f $(GO_DIST_NAME)

gitea-pages: $(shell find . -type f -name "*.go" ) go.mod go.sum
	@CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o $@ .

.PHONY: debug
debug: gitea-pages
	@./gitea-pages -conf config-local.yaml -debug

.PHONY: test
test:
	@go test -v ./...


.PHONY: releases
releases:
	@make release GOOS=linux GOARCH=amd64 && \
	make release GOOS=linux GOARCH=arm64 && \
	make release GOOS=linux GOARCH=loong64 && \
	make release GOOS=windows GOARCH=amd64
