export GOFLAGS := "-overlay=" + justfile_directory() + "/cli/_patches/overlay.json"

overlay:
	cd cli && GOFLAGS= go run _patches/overlay.go >/dev/null

build: overlay
	cd cli && CGO_ENABLED=0 go build -o ../bin/dokky ./cmd/dokky

test: overlay
	cd cli && go test ./...

lint: overlay
	cd cli && golangci-lint run ./...

e2e: overlay
	cd cli && go test -tags e2e ./e2e -count=1 -timeout 30m
