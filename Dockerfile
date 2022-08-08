ARG BASE_IMAGE=docker.io/library/ubuntu:22.04

#################
# Builder layer #
#################
FROM docker.io/library/golang:1.18 as builder

WORKDIR /go/src/github.com/rkrmr33/leader-election

COPY go.mod go.mod
COPY go.sum go.sum

RUN go mod download

COPY . .

RUN go build -o ./dist/leader-elector .

##############
# Base image #
##############
FROM ${BASE_IMAGE} as base

USER root

ENV DEBIAN_FRONENT=noninteractive

RUN groupadd -g 999 leader-elector && \
    useradd -r -u 999 -g leader-elector leader-elector && \
    mkdir -p /home/leader-elector && \
    chown leader-elector:0 /home/leader-elector && \
    chmod g=u /home/leader-elector && \
    apt-get update && \
    apt-get clean && \
    rm -rf /var/lib/apt/lists/* /tmp/* /var/tmp/*

# Copy binary from build step
COPY --from=builder /go/src/github.com/rkrmr33/leader-election/dist/leader-elector /usr/local/bin/


ENV USER=leader-elector
USER 999
WORKDIR /home/leader-elector

ENTRYPOINT [ "leader-elector" ]