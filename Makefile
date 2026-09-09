.PHONY: build test vet fmt run
build:
	go build -trimpath -ldflags "-s -w -X main.version=$${VERSION:-dev}" -o bin/slurm-insights-exporter ./cmd/slurm-insights-exporter
test:
	go test ./...
vet:
	go vet ./...
fmt:
	gofmt -w $$(find . -name '*.go')
run:
	go run ./cmd/slurm-insights-exporter
