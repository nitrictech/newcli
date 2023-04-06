// Copyright Nitric Pty Ltd.
//
// SPDX-License-Identifier: Apache-2.0
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at:
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package runtime

import (
	"bytes"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestGenerate(t *testing.T) {
	tests := []struct {
		name        string
		handler     string
		wantFwriter string
	}{
		{
			name:    "ts",
			handler: "functions/list.ts",
			wantFwriter: `# syntax=docker/dockerfile:1
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

WORKDIR /usr/app

RUN apk update && \
    apk add --no-cache ca-certificates && \
    update-ca-certificates

COPY package.json *.lock *-lock.json ./

RUN set -ex && \
    yarn install --production --frozen-lockfile --cache-folder /tmp/.cache && \
    rm -rf /tmp/.cache

COPY . .

COPY --from=build /usr/app/lib/ ./lib/

# prisma fix for docker installs: https://github.com/prisma/docs/issues/4365
# TODO: remove when custom dockerfile support is available
RUN test -d ./prisma && npx prisma generate || echo "";

ENTRYPOINT ["node", "lib/index.js"]`,
		},
		{
			name:    "go",
			handler: "pkg/handler/list.go",
			wantFwriter: `FROM golang:alpine as build

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

ENTRYPOINT ["/bin/main"]`,
		},
		{
			name:    "python",
			handler: "list.py",
			wantFwriter: `FROM python:3.10-slim

ARG HANDLER

ENV HANDLER=${HANDLER}

RUN apt-get update -y && \
    apt-get install -y ca-certificates && \
    update-ca-certificates

RUN pip install --upgrade pip pipenv

COPY . .

# Guarantee lock file if we have a Pipfile and no Pipfile.lock
RUN (stat Pipfile && pipenv lock) || echo "No Pipfile found"

# Output a requirements.txt file for final module install if there is a Pipfile.lock found
RUN (stat Pipfile.lock && pipenv requirements > requirements.txt) || echo "No Pipfile.lock found"

RUN pip install --no-cache-dir -r requirements.txt

ENTRYPOINT python $HANDLER
`,
		},
		{
			name:    "js",
			handler: "functions/list.js",
			wantFwriter: `# syntax=docker/dockerfile:1
FROM node:alpine

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
  set -ex; \
  yarn install --production --frozen-lockfile --cache-folder /tmp/.cache; \
  rm -rf /tmp/.cache; \
  # prisma fix for docker installs: https://github.com/prisma/docs/issues/4365
  # TODO: remove when custom dockerfile support is available
  test -d ./prisma && npx prisma generate || echo "";

ENTRYPOINT node $HANDLER
`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fwriter := &bytes.Buffer{}
			rt, err := NewRunTimeFromHandler(tt.handler)
			if err != nil {
				t.Error(err)
			}
			if err := rt.BaseDockerFile(fwriter); err != nil {
				t.Errorf("Generate() error = %v", err)
				return
			}
			if !cmp.Equal(fwriter.String(), tt.wantFwriter) {
				t.Error(cmp.Diff(tt.wantFwriter, fwriter.String()))
			}
		})
	}
}
