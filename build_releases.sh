#!/bin/bash

set -x

version="$1"
if [ -z "${version}" ]; then
    echo "Usage: ./build_release.sh [version]"
    exit 1
fi

cd cmd/chunk

os="windows"
for arch in amd64 arm arm64; do
    output="chunk-${version}-${os}-${arch}.exe"
    GOOS=$os GOARCH=$arch go build -o $output
done

os="linux"
for arch in amd64 arm arm64; do
    output="chunk-${version}-${os}-${arch}"
    GOOS=$os GOARCH=$arch go build -o $output
done

os="darwin"
for arch in amd64 arm64; do
    output="chunk-${version}-${os}-${arch}"
    GOOS=$os GOARCH=$arch go build -o $output
done
