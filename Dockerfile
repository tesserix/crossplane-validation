FROM golang:1.26-alpine AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w -X main.version=$(git describe --tags --always --dirty 2>/dev/null || echo dev)" \
    -o operator ./cmd/crossplane-validate-operator

FROM gcr.io/distroless/static:nonroot

COPY --from=builder /app/operator /operator

USER 65534:65534

ENTRYPOINT ["/operator"]
