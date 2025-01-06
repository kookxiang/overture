FROM alpine AS builder

ARG VERSION=1.0

COPY . /src

RUN apk add go && \
    cd /src && \
    go mod download -x && \
    go build -ldflags "-s -w -X main.version=${VERSION} -checklinkname=0" -trimpath -o /bin/overture main/main.go

RUN apk add curl && \
    mkdir -p /config/ip /config/domain && \
    curl -sSL https://github.com/17mon/china_ip_list/raw/refs/heads/master/china_ip_list.txt -o /config/ip/domestic.txt && \
    echo > /config/ip/foreign.txt && \
    curl -sSL https://github.com/felixonmars/dnsmasq-china-list/raw/refs/heads/master/accelerated-domains.china.conf | awk -F'/' '{ print $2 }' > /config/domain/domestic.txt && \
    curl -sSL https://raw.githubusercontent.com/gfwlist/gfwlist/master/gfwlist.txt | base64 -d | grep -E '^\.[a-z0-9_.]+$' | cut -c 2- > /config/domain/foreign.txt && \
    echo > /config/hosts

FROM alpine

COPY --from=builder /bin/overture /bin/overture
COPY --from=builder /config /config
COPY docker/config.yml /config/config.yml

WORKDIR /config

ENTRYPOINT "overture"
