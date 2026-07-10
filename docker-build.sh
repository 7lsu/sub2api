#!/usr/bin/env sh

VERSION=$(cat backend/cmd/server/VERSION)
COMMIT=$(git rev-parse --short HEAD)
DATE=$(date -u +%Y-%m-%dT%H:%M:%SZ)

set -e
cd "$(dirname "$0")"

IMAGE=mikuwithu/sub2api:latest

docker build \
    --platform linux/amd64 \
    -t mikuwithu/sub2api:$VERSION \
    -t mikuwithu/sub2api:latest \
    --build-arg VERSION=$VERSION \
    --build-arg COMMIT=$COMMIT \
    --build-arg DATE=$DATE \
    -f Dockerfile .

[ -n "$1" ] && {
  echo "推送到 $1 ..."
  docker save "$IMAGE" | gzip | ssh "$1" "gunzip | docker load"
  echo "已 load 到 $1"
}

docker push mikuwithu/sub2api:$VERSION
docker push mikuwithu/sub2api:latest

# docker compose -f deploy/docker-compose.miku.yml up