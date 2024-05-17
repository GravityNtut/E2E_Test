#!/bin/sh
mysql -u ${MYSQL_USER} --password=${MYSQL_PASSWORD} -e "USE MirrorTestDB; DELETE FROM Accounts;"
