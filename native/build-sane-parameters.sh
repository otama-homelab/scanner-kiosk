#!/bin/sh
set -eu
umask 077
if [ "$#" -ne 2 ]; then
    echo 'usage: build-sane-parameters.sh SOURCE_DIRECTORY CHECKSUM_FILE' >&2
    exit 2
fi
source_dir=$(realpath "$1")
checksums=$(realpath "$2")
work=$(pwd -P)
mkdir source output home tmp cache modules
# Copy only listed Go inputs; extra files cannot enter the compiler's package.
while read -r expected filename; do
    test "${#expected}" -eq 64
    case "$expected" in *[!0-9a-f]*) exit 1;; esac
    case "$filename" in ''|/*|*[!A-Za-z0-9_./-]*) exit 1;; esac
    case "/$filename/" in *'/../'*|*'/./'*|*'//'*) exit 1;; esac
    case "$filename" in *.go|go.mod|go.sum) ;; *) exit 1;; esac
    mkdir -p "source/$(dirname "$filename")"
    cp "$source_dir/$filename" "source/$filename"
done < "$checksums"
cd source
sha256sum -c "$checksums"
export CGO_ENABLED=1 GOTOOLCHAIN=local GOENV=off GOWORK=off GOFLAGS= GOPROXY=off GOSUMDB=off
export HOME="$work/home" TMPDIR="$work/tmp" GOCACHE="$work/cache" GOMODCACHE="$work/modules"
mkdir -p "$work/output/native/usr/libexec/scanner"
binary="$work/output/native/usr/libexec/scanner/sane-parameters"
go build -mod=readonly -trimpath -buildvcs=false -tags saneparams -o "$binary" ./cmd/sane-parameters > "$work/output/build.log" 2>&1
go version -m "$binary" > "$work/output/buildinfo.txt"
cp /sdk-packages.txt "$work/output/"
tar -cf "$work/native.tar" -C "$work/output" .
sha256sum "$work/native.tar"
echo RESULT_READY
