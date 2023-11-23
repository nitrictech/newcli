# syntax=docker/dockerfile:1
FROM node:alpine as build

ARG HANDLER

# Python and make are required by certain native package build processes in NPM packages.
RUN --mount=type=cache,sharing=locked,target=/var/cache/apk \
    apk --update-cache add git g++ make py3-pip

RUN yarn global add esbuild

WORKDIR /usr/app

COPY package.json *.lock *-lock.json ./

RUN yarn import || echo ""

RUN --mount=type=cache,sharing=locked,target=/tmp/.yarn_cache \
    set -ex && \
    yarn install --production --prefer-offline --frozen-lockfile --cache-folder /tmp/.yarn_cache

RUN test -f tsconfig.json || echo "{\"compilerOptions\":{\"esModuleInterop\":true,\"target\":\"es2015\",\"moduleResolution\":\"node\"}}" > tsconfig.json

COPY . .

# make prisma external to bundle - https://github.com/prisma/prisma/issues/16901#issuecomment-1362940774 \
# TODO: remove when custom dockerfile support is available
RUN --mount=type=cache,sharing=private,target=/tmp/ncc-cache \
  esbuild ./${HANDLER} --bundle --minify --platform=node --outfile=lib/index.js --external:.prisma/client --external:@prisma/client

FROM node:alpine as final

RUN apk update && \
    apk add --no-cache ca-certificates && \
    update-ca-certificates

WORKDIR /usr/app

COPY . .

COPY --from=build /usr/app/node_modules/ ./node_modules/

COPY --from=build /usr/app/lib/ ./lib/

# prisma fix for docker installs: https://github.com/prisma/docs/issues/4365
# TODO: remove when custom dockerfile support is available
RUN test -d ./prisma && npx prisma generate || echo "";

ENTRYPOINT ["node", "lib/index.js"]