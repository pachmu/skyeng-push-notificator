FROM golang as build_base

WORKDIR /skyeng-push-notificator
COPY go.mod go.sum ./

RUN go mod download

FROM build_base as builder

WORKDIR /skyeng-push-notificator

COPY . .

RUN go build -o skyeng-push-notificator cmd/main.go

FROM gcr.io/distroless/base

COPY --from=builder /skyeng-push-notificator/skyeng-push-notificator /

ENTRYPOINT ["/skyeng-push-notificator"]
CMD ["-config", "/etc/skyeng-push-notificator/config.yml"]