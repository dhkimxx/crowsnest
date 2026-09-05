FROM golang:1.24-alpine AS build

WORKDIR /src

COPY go.mod ./
COPY go.sum ./
RUN go mod download
COPY . .

ARG TARGETOS=linux
ARG TARGETARCH
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
	go build -trimpath -ldflags='-s -w' -o /out/crowsnest ./cmd/crowsnest

FROM alpine:3.21

RUN apk add --no-cache ca-certificates && addgroup -S crowsnest && adduser -S -G crowsnest crowsnest && mkdir -p /data && chown crowsnest:crowsnest /data
COPY --from=build /out/crowsnest /usr/local/bin/crowsnest

USER crowsnest
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/crowsnest"]
