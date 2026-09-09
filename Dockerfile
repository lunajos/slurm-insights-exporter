FROM golang:1.24-alpine AS build
WORKDIR /src
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/exporter ./cmd/slurm-insights-exporter

FROM alpine:3.22
RUN apk add --no-cache slurm-client ca-certificates && adduser -D -u 65532 exporter
USER exporter
COPY --from=build /out/exporter /usr/local/bin/slurm-insights-exporter
EXPOSE 9341
ENTRYPOINT ["slurm-insights-exporter"]
