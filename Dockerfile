FROM golang:1.12 as build_base

WORKDIR /skyeng-push-notificator
COPY go.mod go.sum ./

RUN go mod download

FROM build_base as builder

WORKDIR /skyeng-push-notificator
COPY . .

RUN go build

FROM gcr.io/distroless/base

COPY --from=builder /skyeng-push-notificator/skyeng-push-notificator /
COPY --from=builder /skyeng-push-notificator/config.yml /etc/skyeng-push-notificator/config.yml

ENTRYPOINT ["/skyeng-push-notificator"]
CMD ["-config", "/etc/skyeng-push-notificator/config.yml"]