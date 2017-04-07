FROM scratch

WORKDIR /

ADD nginx_request_exporter /nginx_request_exporter

CMD ["/nginx_request_exporter"]
