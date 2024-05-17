#!/bin/sh
/opt/mssql-tools/bin/sqlcmd -U sa -P ${SA_PASSWORD} -e -d TestDB -Q "SELECT * FROM [dbo].[Accounts] ORDER BY [Id] ASC"
