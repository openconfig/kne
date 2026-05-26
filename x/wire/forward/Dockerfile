FROM golang:1.25.8 AS build

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY x/wire/ x/wire/
COPY proto/ proto/

RUN CGO_ENABLED=0 go build -o forward x/wire/forward/main.go

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /app/forward /forward
ENTRYPOINT ["/forward"]
