FROM golang:1.23-alpine AS builder

RUN apk add --no-cache git

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /patchwork .

FROM gcr.io/distroless/static:nonroot

COPY --from=builder /patchwork /patchwork

USER 65532:65532

ENTRYPOINT ["/patchwork"]
