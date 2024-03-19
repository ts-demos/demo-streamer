# syntax=docker/dockerfile:1

FROM golang:1.21-alpine

WORKDIR /app

COPY ui ./ui
COPY go.mod ./
COPY go.sum ./
COPY main.go ./

RUN go mod download
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /demo-streamer

EXPOSE 8080
CMD ["/demo-streamer"]
