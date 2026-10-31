build:
	cd cli && CGO_ENABLED=0 go build -o ../bin/dokky ./cmd/dokky

test:
	cd cli && go test ./...

lint:
	cd cli && golangci-lint run ./...

e2e:
	cd cli && go test -tags e2e ./e2e -count=1 -timeout 30m
