# syntax=docker/dockerfile:1
FROM "node:alpine"

ARG HANDLER
ENV HANDLER=${HANDLER}

RUN apk update && \
    apk add --no-cache ca-certificates && \
    update-ca-certificates

# Python and make are required by certain native package build processes in NPM packages.
ENV PYTHONUNBUFFERED=1
RUN apk add --update --no-cache python3 make g++ && ln -sf python3 /usr/bin/python
RUN python3 -m ensurepip
RUN pip3 install --no-cache --upgrade pip setuptools

COPY . .

RUN yarn import || echo Lockfile already exists

RUN \
--mount=type=cache,target=/node_modules \
set -ex; yarn install --production --frozen-lockfile --cache-folder /tmp/.cache; rm -rf /tmp/.cache

ENTRYPOINT node $HANDLER
