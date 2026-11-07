FROM golang:1.25-alpine AS build

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/telehentai ./cmd/telehentai

FROM alpine:3.22

RUN apk add --no-cache ca-certificates tzdata
COPY --from=build /out/telehentai /telehentai

EXPOSE 8088 80 443
VOLUME ["/data"]

ENTRYPOINT ["/telehentai","run","--config","/config/config.yaml"]
