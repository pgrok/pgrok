FROM golang:1.20.2-alpine3.17 AS binarybuilder
RUN apk --no-cache --no-progress add --virtual build-deps gcc musl-dev

# Install Task
RUN wget --quiet https://github.com/go-task/task/releases/download/v3.12.0/task_linux_amd64.tar.gz -O task_linux_amd64.tar.gz \
  && sh -c 'echo "803d3c1752da31486cbfb4ddf7d8ba5e0fa8c8ebba8acf227a9cd76ff9901343  task_linux_amd64.tar.gz" | sha256sum -c' \
  && tar -xzf task_linux_amd64.tar.gz \
  && mv task /usr/local/bin/task

WORKDIR /dist
COPY . .
RUN task build-pgrokd

FROM alpine:3.15
RUN echo https://dl-cdn.alpinelinux.org/alpine/edge/community/ >> /etc/apk/repositories \
  && apk --no-cache --no-progress add \
  ca-certificates

WORKDIR /app/pgrok/
COPY --from=binarybuilder /dist/.bin/pgrokd .

EXPOSE 3320 3000 2222
CMD ["/app/pgrok/pgrokd"]
