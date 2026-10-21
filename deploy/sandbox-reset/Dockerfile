# The sandbox reset job's image: the ochakai CLI, jq, the seed bundle and
# reset.sh. Build it with build.sh, which assembles the context this file
# expects (./ochakai, ./seed/) from a published release.
FROM alpine:3.22
# ca-certificates is upgraded rather than taken from the base: the image is
# rebuilt from a pinned minor, and a bundle frozen at build time keeps
# trusting whatever its tag shipped with (the eMudhra roots, 2026-10).
RUN apk upgrade --no-cache ca-certificates-bundle && apk add --no-cache jq
COPY ochakai /usr/local/bin/ochakai
COPY seed/ /seed/
COPY reset.sh /usr/local/bin/reset
RUN chmod +x /usr/local/bin/ochakai /usr/local/bin/reset
ENTRYPOINT ["/usr/local/bin/reset"]
