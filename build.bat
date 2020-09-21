@echo off
@cls

git rev-parse HEAD > %temp%\msg.txt
set /p msg= < %temp%\msg.txt

if [%GO_VERSION%] == [] (
	set GO_VERSION=1.15.2
)

set LDFLAG_DEVELOPER=mpetavy
set LDFLAG_HOMEPAGE=https://github.com/mpetavy/netio
set LDFLAG_LICENSE=https://www.apache.org/licenses/LICENSE-2.0.html
set LDFLAG_VERSION=1.0.0
set LDFLAG_EXPIRE=
set LDFLAG_GIT=%msg%
set LDFLAG_COUNTER=1

if [%teamcity.version%] == [] (
    set DOCKER_IMAGE=golang:%GO_VERSION%
) else (
    set DOCKER_IMAGE=artifactory-medmuc:8084/docker-hub/golang:%GO_VERSION%
    docker login --username=%TEAMCITY_USERID% --password=%TEAMCITY_PASSWORD%
)

if exist netio-%LDFLAG_VERSION%-%LDFLAG_COUNTER%-windows-amd64.tar.gz del netio-%LDFLAG_VERSION%-%LDFLAG_COUNTER%-windows-amd64.tar.gz
if exist netio-%LDFLAG_VERSION%-%LDFLAG_COUNTER%-linux-amd64.tar.gz del netio-%LDFLAG_VERSION%-%LDFLAG_COUNTER%-linux-amd64.tar.gz
if exist netio/netio-%LDFLAG_VERSION%-%LDFLAG_COUNTER%-linux-arm6.tar.gz del netio/netio-%LDFLAG_VERSION%-%LDFLAG_COUNTER%-linux-arm6.tar.gz

docker container rm -f netio-builder
docker image rm -f netio-builder

git log ""--pretty=format:"%%h | %%ai | %%s %%d [%%an]""" > CHANGES.txt

docker image build -t netio-builder --target builder --build-arg DOCKER_IMAGE="%DOCKER_IMAGE%" --build-arg LDFLAG_DEVELOPER="%LDFLAG_DEVELOPER%" --build-arg LDFLAG_HOMEPAGE="%LDFLAG_HOMEPAGE%" --build-arg LDFLAG_LICENSE="%LDFLAG_LICENSE%" --build-arg LDFLAG_VERSION="%LDFLAG_VERSION%" --build-arg LDFLAG_EXPIRE="%LDFLAG_EXPIRE%" --build-arg LDFLAG_GIT="%LDFLAG_GIT%" --build-arg LDFLAG_COUNTER="%LDFLAG_COUNTER%" .

del CHANGES.txt

if %errorlevel% == 1 (
    exit /b 1
)

docker create --name netio-builder netio-builder

docker cp netio-builder:/go/src/netio/netio-%LDFLAG_VERSION%-%LDFLAG_COUNTER%-windows-amd64.tar.gz .
docker cp netio-builder:/go/src/netio/netio-%LDFLAG_VERSION%-%LDFLAG_COUNTER%-linux-amd64.tar.gz .
docker cp netio-builder:/go/src/netio/netio-%LDFLAG_VERSION%-%LDFLAG_COUNTER%-linux-arm6.tar.gz .

if not [%teamcity.version%] == [] (
    curl --noproxy "*" -u "$ARTIFACTORY_USERNAME:$ARTIFACTORY_PASSWORD" -H "Content-Type: application/octet-stream" -X PUT "http://artifactory-medmuc:8084/artifactory/netio-release-local/$LDFLAG_VERSION/netio-$LDFLAG_VERSION-$LDFLAG_COUNTER-windows-amd64.tar.gz" -T netio-$LDFLAG_VERSION-$LDFLAG_COUNTER-windows-amd64.tar.gz
    curl --noproxy "*" -u "$ARTIFACTORY_USERNAME:$ARTIFACTORY_PASSWORD" -H "Content-Type: application/octet-stream" -X PUT "http://artifactory-medmuc:8084/artifactory/netio-release-local/$LDFLAG_VERSION/netio-$LDFLAG_VERSION-$LDFLAG_COUNTER-linux-amd64.tar.gz" -T netio-$LDFLAG_VERSION-$LDFLAG_COUNTER-linux-amd64.tar.gz
    curl --noproxy "*" -u "$ARTIFACTORY_USERNAME:$ARTIFACTORY_PASSWORD" -H "Content-Type: application/octet-stream" -X PUT "http://artifactory-medmuc:8084/artifactory/netio-release-local/$LDFLAG_VERSION/netio-$LDFLAG_VERSION-$LDFLAG_COUNTER-linux-arm6.tar.gz" -T netio-$LDFLAG_VERSION-$LDFLAG_COUNTER-linux-arm6.tar.gz
)

docker container rm -f netio-builder
docker image rm -f netio-builder
