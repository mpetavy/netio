#!/bin/sh

set -e

if [ -z "$GO_VERSION" ]; then
  export GO_VERSION="1.15.2"
fi

if [ -z "$TEAMCITY_VERSION" ]; then
  export LDFLAG_DEVELOPER=mpetavy
  export LDFLAG_HOMEPAGE=https://github.com/mpetavy/netio
  export LDFLAG_LICENSE=https://www.apache.org/licenses/LICENSE-2.0.html
  export LDFLAG_VERSION=1.0.0
  export LDFLAG_EXPIRE=
  export LDFLAG_GIT=%msg%
  export LDFLAG_COUNTER=1

  DOCKER_IMAGE=golang:$GO_VERSION
else
  DOCKER_IMAGE=artifactory-medmuc:8084/docker-hub/golang:$GO_VERSION
  # docker login --username=docker --password=ImPIgEFdTcqa4ukVfslX
fi

[ -e netio-$LDFLAG_VERSION-$LDFLAG_COUNTER-windows-amd64.tar.gz ] && rm netio-$LDFLAG_VERSION-$LDFLAG_COUNTER-windows-amd64.tar.gz
[ -e netio-$LDFLAG_VERSION-$LDFLAG_COUNTER-linux-amd64.tar.gz ] && rm netio-$LDFLAG_VERSION-$LDFLAG_COUNTER-linux-amd64.tar.gz
[ -e netio-$LDFLAG_VERSION-$LDFLAG_COUNTER-linux-arm6.tar.gz ] && rm netio-$LDFLAG_VERSION-$LDFLAG_COUNTER-linux-arm6.tar.gz

if [ ! -z "$(docker images -q netio-builder)" ]; then
  docker container rm -f netio-builder
  docker image rm -f netio-builder
fi

git log --pretty=format:"%h | %ai | %s %d [%an]" > CHANGES.txt

docker image build -t netio-builder --target builder --build-arg DOCKER_IMAGE="$DOCKER_IMAGE" --build-arg LDFLAG_DEVELOPER="$LDFLAG_DEVELOPER" --build-arg LDFLAG_HOMEPAGE="$LDFLAG_HOMEPAGE" --build-arg LDFLAG_LICENSE="$LDFLAG_LICENSE" --build-arg LDFLAG_VERSION="$LDFLAG_VERSION" --build-arg LDFLAG_EXPIRE="$LDFLAG_EXPIRE" --build-arg LDFLAG_GIT="$LDFLAG_GIT" --build-arg LDFLAG_COUNTER="$LDFLAG_COUNTER" .

rm CHANGES.txt

docker create --name netio-builder netio-builder

docker cp netio-builder:/go/src/netio/netio-$LDFLAG_VERSION-$LDFLAG_COUNTER-windows-amd64.tar.gz .
docker cp netio-builder:/go/src/netio/netio-$LDFLAG_VERSION-$LDFLAG_COUNTER-linux-amd64.tar.gz .
docker cp netio-builder:/go/src/netio/netio-$LDFLAG_VERSION-$LDFLAG_COUNTER-linux-arm6.tar.gz .

if [ ! -z "$TEAMCITY_VERSION" ]; then
  curl --noproxy "*" -u "$ARTIFACTORY_USERNAME:$ARTIFACTORY_PASSWORD" -H "Content-Type: application/octet-stream" -X PUT "http://artifactory-medmuc:8084/artifactory/netio-release-local/$LDFLAG_VERSION/netio-$LDFLAG_VERSION-$LDFLAG_COUNTER-windows-amd64.tar.gz" -T netio-$LDFLAG_VERSION-$LDFLAG_COUNTER-windows-amd64.tar.gz
  curl --noproxy "*" -u "$ARTIFACTORY_USERNAME:$ARTIFACTORY_PASSWORD" -H "Content-Type: application/octet-stream" -X PUT "http://artifactory-medmuc:8084/artifactory/netio-release-local/$LDFLAG_VERSION/netio-$LDFLAG_VERSION-$LDFLAG_COUNTER-linux-amd64.tar.gz" -T netio-$LDFLAG_VERSION-$LDFLAG_COUNTER-linux-amd64.tar.gz
  curl --noproxy "*" -u "$ARTIFACTORY_USERNAME:$ARTIFACTORY_PASSWORD" -H "Content-Type: application/octet-stream" -X PUT "http://artifactory-medmuc:8084/artifactory/netio-release-local/$LDFLAG_VERSION/netio-$LDFLAG_VERSION-$LDFLAG_COUNTER-linux-arm6.tar.gz" -T netio-$LDFLAG_VERSION-$LDFLAG_COUNTER-linux-arm6.tar.gz
fi

docker cp netio-builder:/go/src/netio/dist-linux-amd64/netio /tmp

lxc delete netio --force
lxc launch images:debian/10 netio
lxc file push /tmp/netio netio/root/
lxc exec netio -- /root/netio -log.verbose -log.file=/root/netio.log -cfg.file=/root/netio.json -app.product=edgebox -callback.noerrors -service=install
lxc exec netio -- systemctl enable netio.service
lxc exec netio -- systemctl start netio.service

# LXD/LXC Version 3

lxc snapshot netio snapshot
lxc publish netio/snapshot --alias netio-snapshot
lxc image export netio-snapshot .
lxc image delete netio-snapshot

mv $(ls -t | head -n1) netio-$LDFLAG_VERSION-$LDFLAG_COUNTER-lxc.tar.gz

# LXD/LXC Version 4

# lxc export netio netio-$LDFLAG_VERSION-$LDFLAG_COUNTER-lxc.tar.gz --instance-only

if [ ! -z "$TEAMCITY_VERSION" ]; then
  curl --noproxy "*" -u "$ARTIFACTORY_USERNAME:$ARTIFACTORY_PASSWORD" -H "Content-Type: application/octet-stream" -X PUT "http://artifactory-medmuc:8084/artifactory/netio-release-local/$LDFLAG_VERSION/netio-$LDFLAG_VERSION-$LDFLAG_COUNTER-lxc.tar.gz" -T netio-$LDFLAG_VERSION-$LDFLAG_COUNTER-lxc.tar.gz
fi

docker container rm -f netio-builder
docker image rm -f netio-builder
