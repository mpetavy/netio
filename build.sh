#!/bin/sh

set -e
set -x

export APP="netio"
export SVC_CMD="-log.verbose -log.file=/root/$APP.log -cfg.file=/root/$APP.json -service=install -s :15000"

git rev-parse HEAD > /tmp/GIT_TAG.txt
read GIT_TAG < /tmp/GIT_TAG.txt

if [ -z "$GO_VERSION" ]; then
  export GO_VERSION="1.16.3"
fi

if [ -z "$TEAMCITY_VERSION" ]; then
  export LDFLAG_DEVELOPER=mpetavy
  export LDFLAG_HOMEPAGE=https://github.com/mpetavy/$APP
  export LDFLAG_LICENSE=https://www.apache.org/licenses/LICENSE-2.0.html
  export LDFLAG_VERSION=1.0.3
  export LDFLAG_EXPIRE=
  export LDFLAG_GIT=$GIT_TAG
  export LDFLAG_COUNTER=1

  DOCKER_IMAGE=golang:$GO_VERSION
else
  DOCKER_IMAGE=artifactory-medmuc:8084/docker-hub/golang:$GO_VERSION
fi

[ -e $APP-$LDFLAG_VERSION-$LDFLAG_COUNTER-windows-amd64.tar.gz ] && rm $APP-$LDFLAG_VERSION-$LDFLAG_COUNTER-windows-amd64.tar.gz
[ -e $APP-$LDFLAG_VERSION-$LDFLAG_COUNTER-linux-amd64.tar.gz ] && rm $APP-$LDFLAG_VERSION-$LDFLAG_COUNTER-linux-amd64.tar.gz
[ -e $APP-$LDFLAG_VERSION-$LDFLAG_COUNTER-linux-arm6.tar.gz ] && rm $APP-$LDFLAG_VERSION-$LDFLAG_COUNTER-linux-arm6.tar.gz

if [ ! -z "$(docker images -q $APP)" ]; then
  docker image rm -f $APP
fi

if [ ! -z "$(docker ps -a | grep -i $APP)" ]; then
  docker container rm -f $APP
fi

git log --pretty=format:"%h | %ai | %s %d [%an]" > CHANGES.txt

docker image build --rm -t $APP --target builder --build-arg DOCKER_IMAGE="$DOCKER_IMAGE" --build-arg LDFLAG_DEVELOPER="$LDFLAG_DEVELOPER" --build-arg LDFLAG_HOMEPAGE="$LDFLAG_HOMEPAGE" --build-arg LDFLAG_LICENSE="$LDFLAG_LICENSE" --build-arg LDFLAG_VERSION="$LDFLAG_VERSION" --build-arg LDFLAG_EXPIRE="$LDFLAG_EXPIRE" --build-arg LDFLAG_GIT="$LDFLAG_GIT" --build-arg LDFLAG_COUNTER="$LDFLAG_COUNTER" .

rm CHANGES.txt

docker create --name $APP $APP

docker cp $APP:/go/src/$APP/$APP-$LDFLAG_VERSION-$LDFLAG_COUNTER-windows-amd64.tar.gz .
docker cp $APP:/go/src/$APP/$APP-$LDFLAG_VERSION-$LDFLAG_COUNTER-linux-amd64.tar.gz .
docker cp $APP:/go/src/$APP/$APP-$LDFLAG_VERSION-$LDFLAG_COUNTER-linux-arm6.tar.gz .

if [ ! -z "$TEAMCITY_VERSION" ]; then
  curl --noproxy "*" -u "$ARTIFACTORY_USERNAME:$ARTIFACTORY_PASSWORD" -H "Content-Type: application/octet-stream" -X PUT "http://artifactory-medmuc:8084/artifactory/$APP-release-local/$LDFLAG_VERSION/$APP-$LDFLAG_VERSION-$LDFLAG_COUNTER-windows-amd64.tar.gz" -T $APP-$LDFLAG_VERSION-$LDFLAG_COUNTER-windows-amd64.tar.gz
  curl --noproxy "*" -u "$ARTIFACTORY_USERNAME:$ARTIFACTORY_PASSWORD" -H "Content-Type: application/octet-stream" -X PUT "http://artifactory-medmuc:8084/artifactory/$APP-release-local/$LDFLAG_VERSION/$APP-$LDFLAG_VERSION-$LDFLAG_COUNTER-linux-amd64.tar.gz" -T $APP-$LDFLAG_VERSION-$LDFLAG_COUNTER-linux-amd64.tar.gz
  curl --noproxy "*" -u "$ARTIFACTORY_USERNAME:$ARTIFACTORY_PASSWORD" -H "Content-Type: application/octet-stream" -X PUT "http://artifactory-medmuc:8084/artifactory/$APP-release-local/$LDFLAG_VERSION/$APP-$LDFLAG_VERSION-$LDFLAG_COUNTER-linux-arm6.tar.gz" -T $APP-$LDFLAG_VERSION-$LDFLAG_COUNTER-linux-arm6.tar.gz
fi

if [ -e "$1"  ] || [ "$OS" == "Windows_NT" ]; then
  echo WARNING! No LXC container is created!
else
  docker cp $APP:/go/src/$APP/dist-linux-amd64/$APP /tmp

  if [ ! -z "$(lxc list | grep -i $APP)" ]; then
    lxc delete $APP --force
  fi

  lxc launch images:debian/10 $APP
  lxc file push /tmp/$APP $APP/root/
  lxc exec $APP -- /root/$APP $SVC_CMD
  lxc exec $APP -- systemctl enable $APP.service
  lxc exec $APP -- systemctl start $APP.service

  # LXD/LXC Version 3

  lxc snapshot $APP snapshot
  lxc publish $APP/snapshot --alias $APP-snapshot
  lxc image export $APP-snapshot .
  lxc image delete $APP-snapshot

  mv $(ls -t | head -n1) $APP-$LDFLAG_VERSION-$LDFLAG_COUNTER-lxc.tar.gz

  # LXD/LXC Version 4

  # lxc export $APP $APP-$LDFLAG_VERSION-$LDFLAG_COUNTER-lxc.tar.gz --instance-only

  if [ ! -z "$TEAMCITY_VERSION" ]; then
    curl --noproxy "*" -u "$ARTIFACTORY_USERNAME:$ARTIFACTORY_PASSWORD" -H "Content-Type: application/octet-stream" -X PUT "http://artifactory-medmuc:8084/artifactory/$APP-release-local/$LDFLAG_VERSION/$APP-$LDFLAG_VERSION-$LDFLAG_COUNTER-lxc.tar.gz" -T $APP-$LDFLAG_VERSION-$LDFLAG_COUNTER-lxc.tar.gz
  fi

  docker container rm -f $APP
  docker image rm -f $APP
fi
