#!/bin/bash

GO=`which go`
REALPATH=`which realpath`
if [ "x$GO" == "x" ]; then
    echo "go missing, please install go 1.5 or newer"
    exit 1
fi

if [ "x$REALPATH" == "x" ]; then
    echo "realpath missing, please install it"
    exit 1
fi

PATH_TO_SCRIPT=`realpath ${0}`
PATH_TO_FOLDER=`dirname "$PATH_TO_SCRIPT"`
PATH_TO_REPO=`cd "${PATH_TO_FOLDER}/.." && pwd -P`

DOCKERFILE="$PATH_TO_FOLDER/Dockerfile"
WATTS_PLUGIN_TESTER="$PATH_TO_REPO/watts-plugin-tester"

cd $PATH_TO_REPO
echo " "
echo " building watts-plugin-tester ..."

VERSION=`go version`
GOPATH=`cd "${PATH_TO_FOLDER}/.." && pwd -P`

echo "    cleaning ..."
pwd
rm watts-plugin-tester
rm watts-plugin-tester_container_*.tgz
echo " "
echo "running the build with '$VERSION', please include in issue reports"
echo " "
export "GOPATH=${GOPATH}"
echo "fetiching:"
echo -n "  kingpin ... "
go get gopkg.in/alecthomas/kingpin.v2
echo "done"
echo -n "  sling ... "
go get github.com/dghubble/sling
echo "done"
echo -n "building watts-plugin-tester ... "
CGO_ENABLED=0 GOOS=linux go build -a -v -o $WATTS_PLUGIN_TESTER ${GOPATH}/watts-plugin-tester.go
echo "done"

echo "building docker ... "
mkdir -p /tmp/watts-plugin-tester_docker/
cp $DOCKERFILE /tmp/watts-plugin-tester_docker/
cp $WATTS_PLUGIN_TESTER /tmp/watts-plugin-tester_docker/
cp /etc/ssl/certs/ca-certificates.crt /tmp/watts-plugin-tester_docker/
cd /tmp/watts-plugin-tester_docker/
WATTS_PLUGIN_TESTER_VERSION=`./watts-plugin-tester version 2>&1`
WATTS_PLUGIN_TESTER_TAG="watts-plugin-tester:$WATTS_PLUGIN_TESTER_VERSION"
WATTS_PLUGIN_TESTER_DOCKER="$PATH_TO_REPO/watts-plugin-tester_container_${WATTS_PLUGIN_TESTER_VERSION}.tar"
docker image rm -f "$WATTS_PLUGIN_TESTER_TAG"
docker build -t "$WATTS_PLUGIN_TESTER_TAG" .
cd $PATH_TO_REPO
rm -rf /tmp/watts-plugin-tester_docker/
docker save --output "$WATTS_PLUGIN_TESTER_DOCKER" "$WATTS_PLUGIN_TESTER_TAG"
echo "done"

echo " "
echo " "
echo " checking image "
docker image rm -f "$WATTS_PLUGIN_TESTER_TAG"
docker images -a
docker load --input "$WATTS_PLUGIN_TESTER_DOCKER"
docker run  --rm "$WATTS_PLUGIN_TESTER_TAG" version
docker images -a
echo " done "
