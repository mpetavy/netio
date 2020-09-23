#!/bin/sh
banner update-vendor

echo This updates the vendor directory and creates also the vendor.tar.gz file

[ -d "./vendor" ] && rm -rf vendor
[ -f "./vendor.tar.gz" ] && rm vendor.tar.gz

go mod vendor
tar -czvf vendor.tar.gz vendor

rm -rf vendor
