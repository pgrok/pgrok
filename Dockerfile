FROM --platform=$BUILDPLATFORM node:24-slim@sha256:24dc26ef1e3c3690f27ebc4136c9c186c3133b25563ae4d7f0692e4d1fe5db0e AS webbuilder
RUN npm install --global corepack@0.31.0
RUN corepack enable

WORKDIR /build
COPY . .
WORKDIR /build/pgrokd/web
RUN pnpm install --frozen-lockfile --prefer-frozen-lockfile \
    && pnpm run build
WORKDIR /build

FROM golang:1.26-alpine3.23@sha256:91eda9776261207ea25fd06b5b7fed8d397dd2c0a283e77f2ab6e91bfa71079d AS binarybuilder
RUN apk --no-cache --no-progress add --virtual \
    build-deps \
    build-base \
    git

ARG BUILD_VERSION="unknown"

WORKDIR /dist
COPY . .
COPY --from=webbuilder /build/pgrokd/cli/internal/web/dist /dist/pgrokd/cli/internal/web/dist
RUN go build -v -trimpath -tags prod \
      -ldflags "-X 'main.version=${BUILD_VERSION}' -X 'main.commit=$(git rev-parse HEAD)' -X 'main.date=$(date -u '+%Y-%m-%d %I:%M:%S %Z')'" \
      -o .bin/pgrokd ./pgrokd/cli

FROM alpine:3.23@sha256:5b10f432ef3da1b8d4c7eb6c487f2f5a8f096bc91145e68878dd4a5019afde11

LABEL org.opencontainers.image.source="https://github.com/pgrok/pgrok"

RUN addgroup --gid 10001 --system nonroot \
  && adduser  --uid 10000 --system --ingroup nonroot --home /home/nonroot nonroot

RUN echo https://dl-cdn.alpinelinux.org/alpine/edge/main/ >> /etc/apk/repositories \
  && echo https://dl-cdn.alpinelinux.org/alpine/edge/community/ >> /etc/apk/repositories \
  && apk --no-cache --no-progress add \
  ca-certificates \
  "curl>8.20.0-r0" \
  tini \
  "zlib>1.3.2"

WORKDIR /app/pgrokd/
COPY --from=binarybuilder /dist/.bin/pgrokd .

USER nonroot
VOLUME ["/var/opt/pgrokd"]
EXPOSE 3320 3000 2222
HEALTHCHECK CMD (curl -o /dev/null -sS http://127.0.0.1:3320/-/healthcheck) || exit 1
ENTRYPOINT ["/sbin/tini", "--", "/app/pgrokd/pgrokd"]
CMD ["--config", "/var/opt/pgrokd/pgrokd.yml"]
