#!/bin/sh

GOOS=darwin GOARCH=amd64 go build -ldflags=-w -o hkmod ./cmd/hkmod &&
zip -j hkmod-macos-amd64.zip hkmod &&
GOOS=darwin GOARCH=arm64 go build -ldflags=-w -o hkmod ./cmd/hkmod &&
zip -j hkmod-macos-arm64.zip hkmod &&
GOOS=linux GOARCH=amd64 go build -ldflags=-w -o hkmod ./cmd/hkmod &&
zip -j hkmod-linux-amd64.zip hkmod &&
GOOS=windows GOARCH=amd64 go build -ldflags=-w -o hkmod ./cmd/hkmod &&
zip -j hkmod-windows-amd64.zip hkmod &&
rm hkmod
