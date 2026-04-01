FROM golang:1.24-alpine AS builder

RUN apk add --no-cache git

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .

ARG VERSION=dev
ARG COMMIT=unknown
ARG BUILD_TIME=unknown

RUN CGO_ENABLED=0 go build \
    -ldflags "-X main.Version=${VERSION} -X main.Commit=${COMMIT} -X main.BuildTime=${BUILD_TIME}" \
    -o /queuekitd ./cmd/queuekitd

RUN CGO_ENABLED=0 go build \
    -ldflags "-X main.Version=${VERSION} -X main.Commit=${COMMIT} -X main.BuildTime=${BUILD_TIME}" \
    -o /queuekit ./cmd/queuekit

FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=builder /queuekitd /queuekitd
COPY --from=builder /queuekit /queuekit

EXPOSE 8080

ENTRYPOINT ["/queuekitd"]
