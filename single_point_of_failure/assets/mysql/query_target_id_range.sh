#!/bin/sh
if [ -z "$1" ] || [ -z "$2" ]; then
    echo "Usage: query_target_id_range.sh <start_id> <end_id>"
    exit 1
fi
mysql -u ${MYSQL_USER} --password=${MYSQL_PASSWORD} -e "USE MirrorTestDB; SELECT * FROM Accounts where id >= ${1} AND id <= ${2} order by id;"