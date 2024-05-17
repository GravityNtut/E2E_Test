#!/bin/sh
/opt/mssql-tools/bin/sqlcmd -U sa -P ${SA_PASSWORD} -e -d TestDB -Q "INSERT INTO Accounts (id, name, phone) VALUES (1000, 'ABC', '9990001111');"
