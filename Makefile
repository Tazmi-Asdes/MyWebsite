.PHONY: fmt generate test vet typecheck build check

ifeq ($(OS),Windows_NT)
EXE_EXT := .exe
MKDIR_P := if not exist .tmp\tools mkdir .tmp\tools
else
EXE_EXT :=
MKDIR_P := mkdir -p .tmp/tools
endif

SQLC_BIN := .tmp/tools/sqlc$(EXE_EXT)
SQLC_BUILD_OUTPUT := ../../.tmp/tools/sqlc$(EXE_EXT)

fmt:
	gofmt -w $$(rg --files cmd internal web/public -g '*.go')

generate:
	$(MKDIR_P)
	go -C tools/sqlc build -o $(SQLC_BUILD_OUTPUT) github.com/sqlc-dev/sqlc/cmd/sqlc
	$(SQLC_BIN) generate -f db/sqlc.yaml
	cd web/admin && npm run generate:api

test:
	go test ./...
	cd web/admin && npm test

vet:
	go vet ./...

typecheck:
	cd web/admin && npm run typecheck

build:
	go build ./...
	cd web/admin && npm run build

check: generate test vet typecheck build
