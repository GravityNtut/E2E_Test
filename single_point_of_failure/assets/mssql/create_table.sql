IF OBJECT_ID('dbo.Accounts', 'U') IS NULL
BEGIN
    CREATE TABLE Accounts (id INT, name NVARCHAR(50), phone NVARCHAR(16));
    CREATE INDEX idx_id ON dbo.Accounts(id);
    EXEC sys.sp_cdc_enable_db;
    EXEC sys.sp_cdc_enable_table
    @source_schema = N'dbo',
    @source_name   = N'Accounts',
    @role_name     = NULL;
    SELECT 'Table `Accounts` created and enabled cdc.';
END
ELSE
BEGIN
    SELECT 'Table `Accounts` already exists.';
END
