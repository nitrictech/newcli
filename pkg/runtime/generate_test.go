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
			wantFwriter: `FROM node:alpine as build

ARG HANDLER

RUN yarn global add typescript @vercel/ncc

COPY package.json *.lock *-lock.json /

RUN yarn import || echo Lockfile already exists

RUN set -ex; yarn install --production --frozen-lockfile --cache-folder /tmp/.cache; rm -rf /tmp/.cache;

COPY . .

RUN test -f tsconfig.json || echo "{\"compilerOptions\":{\"esModuleInterop\":true,\"target\":\"es2015\",\"moduleResolution\":\"node\"}}" > tsconfig.json

RUN ncc build ${HANDLER} -m --v8-cache -o lib/

FROM node:alpine as final

RUN apk update && \
    apk add --no-cache ca-certificates && \
    update-ca-certificates

COPY --from=build "package.json" "package.json"

COPY --from=build "node_modules/" "node_modules/"

COPY --from=build lib/ /

ENTRYPOINT ["node", "index.js"]`,
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
			wantFwriter: `FROM python:3.7-slim

ARG HANDLER

ENV HANDLER=${HANDLER}

RUN apt-get update -y && \
    apt-get install -y ca-certificates && \
    update-ca-certificates

RUN pip install --upgrade pip

COPY requirements.txt requirements.txt

RUN pip install --no-cache-dir -r requirements.txt

COPY . .

ENTRYPOINT python $HANDLER`,
		},
		{
			name:    "js",
			handler: "functions/list.js",
			wantFwriter: `FROM "node:alpine"

ARG HANDLER
ENV HANDLER=${HANDLER}

RUN apk update && \
    apk add --no-cache ca-certificates && \
    update-ca-certificates

COPY package.json *.lock *-lock.json /

RUN yarn import || echo Lockfile already exists

RUN set -ex; yarn install --production --frozen-lockfile --cache-folder /tmp/.cache; rm -rf /tmp/.cache;

COPY . .

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
