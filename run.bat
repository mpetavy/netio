rem WARNING!! OpenSSL 1.1.1 is mandatory

cls

del ca.* /q
del server.* /q
del client.* /q

rem ca.key
openssl genrsa -out ca.key 2048

rem ca.pem
openssl req -new -key ca.key -x509 -days 3650 -out ca.pem -config run.cfg -extensions v3_ca

rem server.key
openssl genrsa -out server.key 2048

rem server.csr
openssl req -new -nodes -key server.key -out server.csr -config run.cfg

rem server.pem
openssl x509 -days 3650 -req -in server.csr -CA ca.pem -CAkey ca.key -CAcreateserial -out server.pem -extfile run.cfg -extensions v3_req

rem client.key
openssl genrsa -out client.key 2048

rem client.csr
openssl req -new -nodes -key client.key -out client.csr -config run.cfg

rem client.pem
openssl x509 -days 3650 -req -in client.csr -CA ca.pem -CAkey ca.key -CAcreateserial -out client.pem -extfile run.cfg -extensions v3_req

rem server.p12
openssl pkcs12 -export -inkey server.key -in server.pem -certfile ca.pem -out server.p12 -passout pass:changeit

rem client.p12
openssl pkcs12 -export -inkey client.key -in client.pem -certfile ca.pem -out client.p12 -passout pass:changeit

rem test - start server
rem netio -s :5000 -log.verbose -tls -tls.p12.mutual=client.p12

rem test - start client
rem netio -c :5000 -log.verbose -tls -tls.p12.file=client.p12
