#############################################
# go workspace
#############################################
FROM debian:12.8-slim

ARG PUID=1000
ENV PUID ${PUID}
ARG PGID=1000
ENV PGID ${PGID}

ENV GO_VERSION 1.23.4

RUN apt-get update -yqq && \
    groupadd -g ${PGID} go && \
    useradd -u ${PUID} -g go -m go -G go && \
    usermod -p "*" go \
    && apt-get install -y wget git

#############################################
# install go
#############################################
RUN cd /tmp && wget --quiet https://golang.org/dl/go${GO_VERSION}.linux-amd64.tar.gz \
    && tar -C /usr/local -xzf go${GO_VERSION}.linux-amd64.tar.gz && rm -rf /tmp/*

ENV GOPATH=/home/go
ENV GOROOT=/usr/local/go/
ENV PATH=$PATH:/usr/local/go/bin:$GOPATH/bin
ENV CGO_ENABLED 0

USER go

WORKDIR /home/go/src