FROM golang:alpine as build

ARG HANDLER

WORKDIR /app/

COPY go.mod *.sum ./

RUN go mod download

COPY . .

RUN go build -o /bin/main ./${HANDLER}/...

FROM alpine

COPY --from=build /bin/main /bin/main

RUN chmod +x-rw /bin/main
RUN apk update && \
    apk add --no-cache tzdata ca-certificates && \
    update-ca-certificates

ENTRYPOINT ["/bin/main"]