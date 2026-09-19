.PHONY: fmt generate test vet typecheck build check

fmt:
	gofmt -w $$(rg --files cmd internal web/public -g '*.go')

generate:
	go tool sqlc generate -f db/sqlc.yaml
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
