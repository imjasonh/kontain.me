#!/usr/bin/env bash

set -euxo pipefail

export KO_DOCKER_REPO=gcr.io/kontaindotme
out=$(mktemp -d)/tmp.tfvars

echo writing to $out

echo "images = {" > $out
cd cmd
for d in *;  do
	echo "  \"$d\" = \"$(ko publish -P ./$d)\"" >> $out
done
echo "}" >> $out

cat $out

cd -

terraform apply -var-file=kontainme.tfvars -var-file=$out
