#!/bin/bash
count=0
until mysql -u$MYSQL_USER -p$MYSQL_PASSWORD -h 127.0.0.1 \
    -e 'USE MirrorTestDB; SELECT count(* )FROM Accounts;' &> /dev/null ; do
    echo "Waiting for initdb finish..$count"
    count=$((count+1))
    if [ $count -gt 90 ]; then
        echo "@@ Accounts table is not ready after 120 seconds"
        exit 1
    fi
    sleep 1
done
echo "E2ETest DB is ready"
exit 0
