#!/bin/bash

set -e

mkdir -p bin

go build -o bin/hips cmd/server/main.go

echo "构建完成，可执行文件位于: bin/hips"