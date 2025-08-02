FROM golang:1.23-alpine as builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go build -o status-checker main.go

FROM alpine:3.19
WORKDIR /app
COPY --from=builder /app/status-checker ./status-checker
COPY --from=builder /app/src/index.html ./index.html
COPY --from=builder /app/uptime.db ./uptime.db
EXPOSE 8080
CMD ["./status-checker"]