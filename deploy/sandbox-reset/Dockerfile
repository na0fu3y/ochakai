# The sandbox reset job's image: the ochakai CLI, jq, the seed bundle and
# reset.sh. Build it with build.sh, which assembles the context this file
# expects (./ochakai, ./seed/) from a published release.
FROM alpine:3.22
RUN apk add --no-cache jq
COPY ochakai /usr/local/bin/ochakai
COPY seed/ /seed/
COPY reset.sh /usr/local/bin/reset
RUN chmod +x /usr/local/bin/ochakai /usr/local/bin/reset
ENTRYPOINT ["/usr/local/bin/reset"]
