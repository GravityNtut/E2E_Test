IF OBJECT_ID('dbo.Products', 'U') IS NULL
BEGIN
    CREATE TABLE Products ( Id INT PRIMARY KEY IDENTITY(1,1) ,Name NVARCHAR(100) NOT NULL, Price DECIMAL(10,2) NOT NULL, Stock INT NOT NULL, Obsolete BIT NOT NULL DEFAULT 0 , ModTime DATETIME not null );
    CREATE INDEX idx_id ON dbo.Products(Id);
    EXEC sys.sp_cdc_enable_db;
    EXEC sys.sp_cdc_enable_table
    @source_schema = N'dbo',
    @source_name   = N'Products',
    @role_name     = NULL;
    SELECT 'Table `Products` created and enabled cdc.';
END
ELSE
BEGIN
    SELECT 'Table `Products` already exists.';
END
