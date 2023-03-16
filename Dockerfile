FROM golang:1.20-alpine3.17 AS binarybuilder
RUN apk --no-cache --no-progress add --virtual \
    build-deps \
    build-base \
    git

# Install Task
RUN export url="https://github.com/go-task/task/releases/download/v3.22.0/task_linux_"; \
  if [ `uname -m` == "aarch64" ]; then \
       export arch='arm64' \
    && wget --quiet ${url}${arch}.tar.gz -O task_linux_${arch}.tar.gz \
    && sh -c 'echo "9827e63054ddec1ffe0f246f9bb0c0de0d30deac2055481b44304d13cc928fe2  task_linux_${arch}.tar.gz" | sha256sum -c'; \
  elif [ `uname -m` == "armv7l" ]; then \
       export arch='arm' \
    && wget --quiet ${url}${arch}.tar.gz -O task_linux_${arch}.tar.gz \
    && sh -c 'echo "068793abf6b6c18bfcc9f22207b12de7f25d922960cd5b48e3547851216bc456  task_linux_${arch}.tar.gz" | sha256sum -c'; \
  else \
       export arch='amd64' \
    && wget --quiet ${url}${arch}.tar.gz -O task_linux_${arch}.tar.gz \
    && sh -c 'echo "1079079045b66cde89827c0129aff180ad2d67fda71415164a2a3e98f37c40e7  task_linux_${arch}.tar.gz" | sha256sum -c'; \
  fi \
  && tar -xzf task_linux_${arch}.tar.gz \
  && mv task /usr/local/bin/task

ARG BUILD_VERSION="unknown"

WORKDIR /dist
COPY . .
RUN BUILD_VERSION=${BUILD_VERSION} task build-pgrokd-release

FROM alpine:3.17

RUN addgroup --gid 10001 --system nonroot \
  && adduser  --uid 10000 --system --ingroup nonroot --home /home/nonroot nonroot

RUN echo https://dl-cdn.alpinelinux.org/alpine/edge/community/ >> /etc/apk/repositories \
  && apk --no-cache --no-progress add \
  ca-certificates \
  curl \
  tini

WORKDIR /app/pgrokd/
COPY --from=binarybuilder /dist/pgrokd .

USER nonroot
VOLUME ["/var/opt/pgrokd"]
EXPOSE 3320 3000 2222
HEALTHCHECK CMD (curl -o /dev/null -sS http://127.0.0.1:3320/-/healthcheck) || exit 1
ENTRYPOINT ["/sbin/tini", "--", "/app/pgrokd/pgrokd"]
CMD ["--config", "/var/opt/pgrokd/pgrokd.yml"]
