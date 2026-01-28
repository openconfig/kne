FROM golang:1.25

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY x/wire/ x/wire/
COPY proto/ proto/

RUN go build -o forward x/wire/forward/main.go

ENTRYPOINT ["./forward"]
