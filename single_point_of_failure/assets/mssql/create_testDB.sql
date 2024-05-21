IF DB_ID('TestDB') IS NULL
BEGIN
    CREATE DATABASE TestDB;
    SELECT 'Database created.';
END
ELSE
BEGIN
    SELECT 'Database `TestDB` already exists.';
END