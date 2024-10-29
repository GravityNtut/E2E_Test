Feature: Gravity2 MSSQL to MySQL - Check boolean type 
    Background: Set up check boolean type test
        Given Create all services
        Given Load the initial configuration file
        Given Start the "source-mssql" service (timeout "60")
        Given Initialize the "source-mssql" table Products
        Given Start the "target-mysql" service (timeout "60")
        Given Initialize the "target-mysql" table Products
        Given Start the "nats-jetstream" service (timeout "60")
        Given Start the "gravity-dispatcher" service (timeout "60")
        Given Create data product Products
        Given Set up atomic flow document
        Given Start the "atomic" service (timeout "60")
        Given Start the "gravity-adapter-mssql" service (timeout "60")
        
    Scenario: Check the boolean value correctness at each point in the process
        Given "source-mssql" table "Products" inserted a record which has false boolean value
        Then Check the "source-mssql" table Products has a record with false value
        Then Check the nats stream default domain has a record with false value
        Then Check the subscribe sdk result has a record with false value
        Then Check the "target-mysql" table Products has a record with false value