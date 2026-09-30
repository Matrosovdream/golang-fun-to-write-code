# Multi-stage: the build image is ~1GB, the final image ~15MB.
FROM golang:1.26-alpine AS build
WORKDIR /src
# Copying go.mod/go.sum first keeps the dependency download in its own
# layer cache — code changes don't re-download modules.
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /bin/gobank ./cmd/server

FROM alpine:3.20
RUN adduser -D -u 10001 app
USER app
COPY --from=build /bin/gobank /bin/gobank
EXPOSE 8081
ENTRYPOINT ["/bin/gobank"]
