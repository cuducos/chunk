FROM golang:1.25-trixie as build
WORKDIR /chunk
COPY go.* .
RUN go mod download
COPY *.go .
RUN go build -o /usr/bin/chunk

FROM debian:trixie-slim
COPY --from=build /usr/bin/chunk /usr/bin/chunk
CMD ["chunk"]
