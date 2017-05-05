FROM golang:alpine

RUN mkdir -p /go/src/nginx_request_exporter
WORKDIR /go/src/nginx_request_exporter

COPY . /go/src/nginx_request_exporter

RUN apk add --no-cache --virtual .git git ; go-wrapper download ; apk del .git
RUN go-wrapper install

EXPOSE 9147 9514/udp
USER nobody
ENTRYPOINT ["nginx_request_exporter"]
