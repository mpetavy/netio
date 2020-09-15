docker-compose rm -f -s
docker-compose up --build --force-recreate --remove-orphans

docker-compose down --rmi all -v