FROM alpine:3.14

COPY bin/gateway-extension .

ENTRYPOINT ["/gateway-extension"]
