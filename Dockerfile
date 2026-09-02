FROM golang:1.26-alpine AS build
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
RUN go run github.com/a-h/templ/cmd/templ@v0.3.1020 generate && go build -o files ./cmd

FROM alpine:3.22
COPY --from=build /app/files /files
ENV FILES_ROOT=/data
VOLUME /data
EXPOSE 3000
ENTRYPOINT ["/files"]
