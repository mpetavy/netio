#!/bin/sh

# to build this image: ./build.sh
# to run this image: docker run --env netio_limit_netio_port=9999 --name netio-app netio-app

set -e # stop on error in script
set -x # log script steps to STDOUT

export APP="netio"
export SVC_CMD="-log.verbose -log.file=/root/$APP.log -cfg.file=/root/$APP.json -service=install"

git rev-parse HEAD >/tmp/GIT_TAG.txt
read GIT_TAG </tmp/GIT_TAG.txt

if [ -z "$GO_VERSION" ]; then
  export GO_VERSION="1.17"
fi

if [ -z "$TEAMCITY_VERSION" ]; then
  export LDFLAG_DEVELOPER=mpetavy
  export LDFLAG_HOMEPAGE=https://github.com/mpetavy/$APP
  export LDFLAG_LICENSE=https://www.apache.org/licenses/LICENSE-2.0.html
  export LDFLAG_VERSION=1.0.11
  export LDFLAG_EXPIRE=
  export LDFLAG_GIT=$(git rev-parse HEAD)
  export LDFLAG_BUILD="1"

  export DOCKER_IMAGE=golang:$GO_VERSION
else
  export DOCKER_IMAGE=artifactory-medmuc:8084/docker-hub/golang:$GO_VERSION
fi

[ -e $APP-$LDFLAG_VERSION-$LDFLAG_BUILD-windows-amd64.tar.gz ] && rm $APP-$LDFLAG_VERSION-$LDFLAG_BUILD-windows-amd64.tar.gz
[ -e $APP-$LDFLAG_VERSION-$LDFLAG_BUILD-linux-amd64.tar.gz ] && rm $APP-$LDFLAG_VERSION-$LDFLAG_BUILD-linux-amd64.tar.gz
[ -e $APP-$LDFLAG_VERSION-$LDFLAG_BUILD-linux-arm6.tar.gz ] && rm $APP-$LDFLAG_VERSION-$LDFLAG_BUILD-linux-arm6.tar.gz

if [ ! -z "$(docker images -q $APP)" ]; then
  docker image rm -f $APP
fi

if [ ! -z "$(docker ps -a | grep -i $APP)" ]; then
  docker container rm -f $APP
fi

git log --pretty=format:"%h | %ai | %s %d [%an]" >CHANGES.txt

docker image build --force-rm --rm -t $APP --target builder --build-arg DOCKER_IMAGE="$DOCKER_IMAGE" --build-arg LDFLAG_DEVELOPER="$LDFLAG_DEVELOPER" --build-arg LDFLAG_HOMEPAGE="$LDFLAG_HOMEPAGE" --build-arg LDFLAG_LICENSE="$LDFLAG_LICENSE" --build-arg LDFLAG_VERSION="$LDFLAG_VERSION" --build-arg LDFLAG_EXPIRE="$LDFLAG_EXPIRE" --build-arg LDFLAG_GIT="$LDFLAG_GIT" --build-arg LDFLAG_BUILD="$LDFLAG_BUILD" .

rm CHANGES.txt

docker create --name $APP $APP

docker cp $APP:/go/src/$APP/$APP-$LDFLAG_VERSION-$LDFLAG_BUILD-windows-amd64.tar.gz .
docker cp $APP:/go/src/$APP/$APP-$LDFLAG_VERSION-$LDFLAG_BUILD-linux-amd64.tar.gz .
docker cp $APP:/go/src/$APP/$APP-$LDFLAG_VERSION-$LDFLAG_BUILD-linux-arm6.tar.gz .

if [ -z "$TEAMCITY_VERSION" ]; then
  if [ ! -z "$(docker images -q $APP-app)" ]; then
    docker image rm -f $APP-app
  fi

  if [ ! -z "$(docker ps -a | grep -i $APP-app)" ]; then
    docker container rm -f $APP-app
  fi

  docker cp $APP:/go/src/$APP/dist-linux-amd64/netio .

  docker image build --force-rm -t $APP-app -f ./Dockerfile-run .

  rm ./netio

  # docker login -u $DOCKER_USERNAME -p $DOCKER_PASSWORD
  # docker tag netio-app mpetavy/netio-app
  # docker commit -m "netio-app" -a mpetavy `docker container ls -lq` mpetavy/netio-app
  # docker push mpetavy/netio-app
fi

if [ ! -z "$TEAMCITY_VERSION" ]; then
  curl --noproxy "*" -u "$ARTIFACTORY_USERNAME:$ARTIFACTORY_PASSWORD" -H "Content-Type: application/octet-stream" -X PUT "http://artifactory-medmuc:8084/artifactory/$APP-release-local/$LDFLAG_VERSION/$APP-$LDFLAG_VERSION-$LDFLAG_BUILD-windows-amd64.tar.gz" -T $APP-$LDFLAG_VERSION-$LDFLAG_BUILD-windows-amd64.tar.gz
  curl --noproxy "*" -u "$ARTIFACTORY_USERNAME:$ARTIFACTORY_PASSWORD" -H "Content-Type: application/octet-stream" -X PUT "http://artifactory-medmuc:8084/artifactory/$APP-release-local/$LDFLAG_VERSION/$APP-$LDFLAG_VERSION-$LDFLAG_BUILD-linux-amd64.tar.gz" -T $APP-$LDFLAG_VERSION-$LDFLAG_BUILD-linux-amd64.tar.gz
  curl --noproxy "*" -u "$ARTIFACTORY_USERNAME:$ARTIFACTORY_PASSWORD" -H "Content-Type: application/octet-stream" -X PUT "http://artifactory-medmuc:8084/artifactory/$APP-release-local/$LDFLAG_VERSION/$APP-$LDFLAG_VERSION-$LDFLAG_BUILD-linux-arm6.tar.gz" -T $APP-$LDFLAG_VERSION-$LDFLAG_BUILD-linux-arm6.tar.gz
fi

if [ "#$OS#" = '#Windows_NT#' -o "#$BUILD_LXC#" = '##' -o "#$(which lxc)#" = '##' ]; then
  echo WARNING! No LXC container is created!

  exit 0
fi

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

mv $(ls -t | head -n1) $APP-$LDFLAG_VERSION-$LDFLAG_BUILD-lxc.tar.gz

# LXD/LXC Version 4

# lxc export $APP $APP-$LDFLAG_VERSION-$LDFLAG_BUILD-lxc.tar.gz --instance-only

if [ ! -z "$TEAMCITY_VERSION" ]; then
  curl --noproxy "*" -u "$ARTIFACTORY_USERNAME:$ARTIFACTORY_PASSWORD" -H "Content-Type: application/octet-stream" -X PUT "http://artifactory-medmuc:8084/artifactory/$APP-release-local/$LDFLAG_VERSION/$APP-$LDFLAG_VERSION-$LDFLAG_BUILD-lxc.tar.gz" -T $APP-$LDFLAG_VERSION-$LDFLAG_BUILD-lxc.tar.gz
fi

docker container rm -f $APP
docker image rm -f $APP
