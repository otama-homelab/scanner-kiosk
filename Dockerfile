# Experimental: input must be the complete bundled executable, not the API-only binary.
FROM gcr.io/distroless/static-debian13:nonroot@sha256:25ac8f0c3b2334a9eb05452266bd4697864a8c8ef0ef71e5e6480d7f92868329
COPY --chmod=0555 scanner /usr/local/bin/scanner
USER 10001:10001
ENV XDG_CACHE_HOME=/tmp
WORKDIR /tmp
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/scanner"]
CMD ["serve"]
