.PHONY: build test vet fmt run release
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
release:
	./scripts/build-release.sh $${VERSION:-0.1.0}
