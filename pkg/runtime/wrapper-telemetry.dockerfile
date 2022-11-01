ARG BASE_IMAGE

FROM ${BASE_IMAGE}

ARG MEMBRANE_URI
ARG MEMBRANE_VERSION

ENV MEMBRANE_VERSION ${MEMBRANE_VERSION}

ADD ${MEMBRANE_URI} /bin/membrane

RUN chmod +x-rw /bin/membrane

ARG OTELCOL_CONTRIB_URI

ADD ${OTELCOL_CONTRIB_URI} /usr/bin/
RUN tar -xzf /usr/bin/otelcol*.tar.gz &&\
    rm /usr/bin/otelcol*.tar.gz &&\
	mv /otelcol-contrib /usr/bin/

ARG OTELCOL_CONFIG
COPY ${OTELCOL_CONFIG} /etc/otelcol/config.yaml
RUN chmod -R a+r /etc/otelcol

ARG NITRIC_TRACE_SAMPLE_PERCENT
ENV NITRIC_TRACE_SAMPLE_PERCENT ${NITRIC_TRACE_SAMPLE_PERCENT}

CMD [%s]
ENTRYPOINT ["/bin/membrane"]
