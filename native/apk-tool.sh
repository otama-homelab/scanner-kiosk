#!/bin/sh
# Alpine's verifier uses its own loader/libraries inside the Debian Python tool.
exec /apk/lib/ld-musl-x86_64.so.1 --library-path /apk/lib:/apk/usr/lib /apk/sbin/apk "$@"
