FROM python:3.7-slim

ARG HANDLER

ENV HANDLER=${HANDLER}

RUN apt-get update -y && \
    apt-get install -y ca-certificates && \
    update-ca-certificates

RUN pip install --upgrade pip

COPY requirements.txt requirements.txt

RUN pip install --no-cache-dir -r requirements.txt

COPY . .

ENTRYPOINT python $HANDLER