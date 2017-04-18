#!/bin/bash

PACKAGE=github.com/indigo-dc/watts-plugin-tester

if ! which go >/dev/null
then
	echo "go missing, please install go 1.5 or newer"
	exit 1
fi

if [[ -z $GOPATH ]]
then
	export GOPATH=`pwd -P`/gopath
	echo using local GOPATH $GOPATH
fi

VERSION=`go version`
echo "running the build with '$VERSION', please include in issue reports"

echo "fetching:"
go get $PACKAGE
echo "done"

echo "building:"
go build -o watts-plugin-tester $PACKAGE
echo "done"
