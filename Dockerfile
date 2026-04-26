FROM golang:1.22-alpine AS builder
WORKDIR /build
COPY go.mod main.go ./
RUN go build -o discpin .

FROM alpine:3.20
WORKDIR /app
COPY --from=builder /build/discpin .
COPY README.md .
EXPOSE 8090
ENTRYPOINT ["./discpin"]
