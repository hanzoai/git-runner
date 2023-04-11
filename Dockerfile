FROM golang:alpine as builder
RUN apk add --update-cache make git

COPY . /opt/src/act_runner
WORKDIR /opt/src/act_runner

RUN make clean && make build

FROM alpine as runner
RUN apk add --update-cache \
    git bash \
    && rm -rf /var/cache/apk/*

COPY --from=builder /opt/src/act_runner/act_runner /usr/local/bin/act_runner
COPY run.sh /opt/act/run.sh

ENTRYPOINT ["/opt/act/run.sh"]
