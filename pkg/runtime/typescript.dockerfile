# syntax=docker/dockerfile:1
FROM node:alpine as build

ARG HANDLER

# Python and make are required by certain native package build processes in NPM packages.
RUN apk add g++ make py3-pip

RUN yarn global add typescript @vercel/ncc

WORKDIR /usr/app

COPY package.json *.lock *-lock.json /

RUN yarn import || echo ""

RUN set -ex && \
    yarn install --production --frozen-lockfile --cache-folder /tmp/.cache && \
    rm -rf /tmp/.cache

RUN test -f tsconfig.json || echo "{\"compilerOptions\":{\"esModuleInterop\":true,\"target\":\"es2015\",\"moduleResolution\":\"node\"}}" > tsconfig.json

COPY . .

# make prisma external to bundle - https://github.com/prisma/prisma/issues/16901#issuecomment-1362940774 \
# TODO: remove when custom dockerfile support is available
RUN \
  --mount=type=cache,target=/tmp/ncc-cache \
  ncc build ${HANDLER} -o lib/ -e .prisma/client -e @prisma/client -t

FROM node:alpine as final

RUN apk update && \
    apk add --no-cache ca-certificates && \
    update-ca-certificates

WORKDIR /usr/app

COPY package.json *.lock *-lock.json ./

RUN set -ex && \
    yarn install --production --frozen-lockfile --cache-folder /tmp/.cache && \
    rm -rf /tmp/.cache

COPY . .

COPY --from=build /usr/app/lib/ ./lib/

# prisma fix for docker installs: https://github.com/prisma/docs/issues/4365
# TODO: remove when custom dockerfile support is available
RUN test -d ./prisma && npx prisma generate || echo "";

ENTRYPOINT ["node", "lib/index.js"]