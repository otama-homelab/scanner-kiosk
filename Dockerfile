# Inputs are prepared and hash-checked by the runtime image build pipeline.
FROM gcr.io/distroless/static-debian13:nonroot@sha256:25ac8f0c3b2334a9eb05452266bd4697864a8c8ef0ef71e5e6480d7f92868329
LABEL org.opencontainers.image.source="https://github.com/otama-homelab/scanner-kiosk"
COPY --chmod=0555 scanner /usr/local/bin/scanner
COPY native/lib/ /usr/lib/
COPY native/usr/ /usr/
COPY --chmod=0444 native-manifest.json /usr/share/scanner-kiosk/runtime-manifest.json
COPY --chmod=0444 native-provenance.json /usr/share/scanner-kiosk/runtime-provenance.json
COPY --chmod=0444 LICENSE /usr/share/doc/scanner-kiosk/LICENSE
USER 10001:10001
ENV XDG_CACHE_HOME=/tmp
ENV SSL_CERT_FILE=/usr/share/scanner-kiosk/ca-certificates.crt SCANNER_OCR_PLUGINS=""
WORKDIR /tmp
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/scanner"]
CMD ["serve"]
