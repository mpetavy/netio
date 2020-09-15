#/bin/sh
tar -zxvf netio-1.0.0-1234-linux-amd64.tar.gz -C /tmp ./netio
lxc delete netio --force
lxc launch images:debian/10 netio
lxc file push /tmp/netio netio/root/
lxc exec netio -- /root/netio -log.verbose -log.file=/root/netio.log -cfg.file=/root/netio.json -app.product=edgebox -callback.ignore -service=install
lxc exec netio -- systemctl enable netio.service
lxc exec netio -- systemctl start netio.service
# lxc exec netio -- /bin/bash
lxc export netio netio-1.0.0-1234-lxc.tar.gz --instance-only
