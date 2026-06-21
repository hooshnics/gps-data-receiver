#!/bin/bash

func=$1
version=$2

if [ "$func" = "public" ]; then
    echo "Start $func:"
    git checkout v4
    git pull
    git tag -a "$version" -m "Releasing version $version"
    git push origin "$version"
    export GOPROXY=proxy.golang.org
    go list -m github.com/ismhdez/socket.io-golang/v4@"$version"
    echo "Done $func"
fi