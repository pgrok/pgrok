FROM node:22-slim AS webbuilder
RUN npm install --global corepack@0.31.0
RUN corepack enable

WORKDIR /build
COPY . .
RUN pnpm --dir pgrokd/web install --frozen-lockfile --prefer-frozen-lockfile \
    && pnpm --dir pgrokd/web run build

FROM golang:alpine3.23 AS binarybuilder
RUN apk --no-cache --no-progress add --virtual \
    build-deps \
    build-base \
    git

# Install Task
RUN export url="https://github.com/go-task/task/releases/download/v3.40.1/task_linux_"; \
  if [ `uname -m` == "aarch64" ]; then \
       export arch='arm64' \
    && wget --quiet ${url}${arch}.tar.gz -O task_linux_${arch}.tar.gz \
    && sh -c 'echo "17f325293d08f6f964e0530842e9ef1410dd5f83ee6475b493087391032b0cfd  task_linux_${arch}.tar.gz" | sha256sum -c'; \
  elif [ `uname -m` == "armv7l" ]; then \
       export arch='arm' \
    && wget --quiet ${url}${arch}.tar.gz -O task_linux_${arch}.tar.gz \
    && sh -c 'echo "e5b0261e9f6563ce3ace9e038520eb59d2c77c8d85f2b47ab41e1fe7cf321528  task_linux_${arch}.tar.gz" | sha256sum -c'; \
  else \
       export arch='amd64' \
    && wget --quiet ${url}${arch}.tar.gz -O task_linux_${arch}.tar.gz \
    && sh -c 'echo "a35462ec71410cccfc428072de830e4478bc57a919d0131ef7897759270dff8f  task_linux_${arch}.tar.gz" | sha256sum -c'; \
  fi \
  && tar -xzf task_linux_${arch}.tar.gz \
  && mv task /usr/local/bin/task

ARG BUILD_VERSION="unknown"

WORKDIR /dist
COPY . .
COPY --from=webbuilder /build/pgrokd/cli/dist /dist/pgrokd/cli/dist
RUN BUILD_VERSION=${BUILD_VERSION} task build-pgrokd-release

FROM alpine:3.23

LABEL org.opencontainers.image.source = "https://github.com/pgrok/pgrok"

RUN addgroup --gid 10001 --system nonroot \
  && adduser  --uid 10000 --system --ingroup nonroot --home /home/nonroot nonroot

RUN echo https://dl-cdn.alpinelinux.org/alpine/edge/community/ >> /etc/apk/repositories \
  && apk --no-cache --no-progress add \
  ca-certificates \
  curl \
  tini

WORKDIR /app/pgrokd/
COPY --from=binarybuilder /dist/.bin/pgrokd .

USER nonroot
VOLUME ["/var/opt/pgrokd"]
EXPOSE 3320 3000 2222
HEALTHCHECK CMD (curl -o /dev/null -sS http://127.0.0.1:3320/-/healthcheck) || exit 1
ENTRYPOINT ["/sbin/tini", "--", "/app/pgrokd/pgrokd"]
CMD ["--config", "/var/opt/pgrokd/pgrokd.yml"]
