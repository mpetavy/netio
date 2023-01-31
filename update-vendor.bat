@echo off
cls

banner update-vendor

echo This updates the vendor directory and creates also the vendor.tar.gz file

if exist vendor rd vendor /s /q
if exist vendor.tar.gz del vendor.tar.gz

go mod vendor
tar -czvf vendor.tar.gz vendor

rd vendor /s /q
