FROM golang:1.26-alpine AS build

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .

ARG APP
RUN test -n "$APP"
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/safeconfig ./cmd/${APP}

FROM alpine:3.22

RUN addgroup -S safeconfig && adduser -S safeconfig -G safeconfig
USER safeconfig

COPY --from=build /out/safeconfig /usr/local/bin/safeconfig

ENTRYPOINT ["/usr/local/bin/safeconfig"]
