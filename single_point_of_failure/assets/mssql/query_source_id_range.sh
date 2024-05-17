#!/bin/sh
if [ -z "$1" ] || [ -z "$2" ]; then
    echo "Usage: query_source_id_range.sh <start_id> <end_id>"
    exit 1
fi
/opt/mssql-tools/bin/sqlcmd -U sa -P ${SA_PASSWORD} -e -d TestDB -Q "SELECT * FROM [dbo].[Accounts] WHERE id >= ${1} AND id <= ${2}"
