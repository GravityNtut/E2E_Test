Feature: Gravity2 MSSQL to MySQL - No data changes - Component restart
    Scenario: Initialize the test environment
        Given Create all services
        Given Load the initial configuration file
        Given Start the "source-mssql" service (timeout "60")
        Given Initialize the "source-mssql" table Accounts
        Given Start the "target-mysql" service (timeout "60")
        Given Initialize the "target-mysql" table Accounts
        Given Start the "nats-jetstream" service (timeout "60")
        Given Start the "gravity-dispatcher" service (timeout "60")
        Given Create Data Product Accounts
        Given Set up atomic flow document
        Given Start the "atomic" service (timeout "60")
        Given Start the "gravity-adapter-mssql" service (timeout "60")
        
    Scenario: After synchronizing changes, restart the component, wait for it to start, then add/update/delete data
        Then "source-mssql" table "Accounts" has 0 records (timeout "3")
        Given "source-mssql" table "Accounts" added "1000" records (starting ID "1")
        Then "target-mysql" table "Accounts" matches "source-mssql" in both record count and content (timeout "90")
        Given docker compose "stop" service "<RestartService>" (in "foreground")
        Then container "<RestartService>" was "exited" (timeout "120")
        Given docker compose "start" service "<RestartService>" (in "foreground")
        When container "<RestartService>" ready (timeout "120")
        Given "source-mssql" table "Accounts" updated "1000" records - appending suffix 'updated' to each Name field (starting ID "1")
        Given "source-mssql" table "Accounts" added "1000" records (starting ID "1001")
        Then "target-mysql" table "Accounts" matches "source-mssql" in both record count and content (timeout "90")
        Given "source-mssql" table "Accounts" cleared
        Then "target-mysql" table "Accounts" has 0 records (timeout "120")

        Examples:
            |   RestartService      |
            | gravity-adapter-mssql |
            | gravity-dispatcher    | 
            | nats-jetstream        |
            | atomic                |
            | source-mssql          |
            | target-mysql          |

    Scenario: Clear the test environment
        Given Close all services