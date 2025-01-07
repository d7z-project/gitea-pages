fmt:
	@(test -f "$(GOPATH)/bin/gofumpt" || go install mvdan.cc/gofumpt@latest) && \
	"$(GOPATH)/bin/gofumpt" -l -w .

gitea-pages: $(shell find . -type f -name "*.go" ) go.mod go.sum
	@CGO_ENABLED=0 go build -o $@ .

.PHONY: debug

debug: gitea-pages
	@./gitea-pages -conf config-local.yaml -debug