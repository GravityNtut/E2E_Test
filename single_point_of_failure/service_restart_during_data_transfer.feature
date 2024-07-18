Feature: Gravity2 MSSQL to MySQL - Service restart during data transfer
    Background: Set up single point of failure test
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
        
    Scenario Outline: 資料傳輸期間重啟元件，等待元件啟動完成，接著新增/更新/刪除資料
        Then "source-mssql" table "Accounts" has "0" datas (timeout "3")
        Given "source-mssql" 資料表 "Accounts" 開始持續新增 "3000" 筆 (ID 開始編號 "1")
        Given docker compose "stop" service "<RestartService>" (in "foreground")
        Then container "<RestartService>" was "exited" (timeout "120")
        # Then Wait "20" seconds
        Given docker compose "start" service "<RestartService>" (in "foreground")
        When container "<RestartService>" ready (timeout "120")
        Then 等待 "source-mssql" 資料表 "Accounts" 新增完成 (timeout "120")
        And "target-mysql" has the same content as "source-mssql" in "Accounts" (timeout "90")
        Given "source-mssql" 資料表 "Accounts" 開始持續更新 "3000" 筆 - 每筆 Name 的內容加上後綴 updated (ID 開始編號 "1") 並新增 "1000" 筆 (ID 開始編號 "3001")
        Given docker compose "stop" service "<RestartService>" (in "foreground")
        Then container "<RestartService>" was "exited" (timeout "120")
        # Then Wait "20" seconds
        Given docker compose "start" service "<RestartService>" (in "foreground")
        When container "<RestartService>" ready (timeout "120")
        Then 等待 "source-mssql" 資料表 "Accounts" 更新完成及新增完成 (timeout "120")
        And "target-mysql" has the same content as "source-mssql" in "Accounts" (timeout "90")
        Given "source-mssql" 資料表 "Accounts" 開始持續清空
        Given docker compose "stop" service "<RestartService>" (in "foreground")
        Then container "<RestartService>" was "exited" (timeout "120")
        # Then Wait "20" seconds
        Given docker compose "start" service "<RestartService>" (in "foreground")
        When container "<RestartService>" ready (timeout "120")
        Then 等待 "source-mssql" 資料表 "Accounts" 清空完成 (timeout "120")
        And "target-mysql" table "Accounts" has "0" datas (timeout "120")
        
        Examples:
            |   RestartService      |
            | gravity-adapter-mssql |
            | gravity-dispatcher    | 
            | nats-jetstream        |
            | atomic                |
            | target-mysql          |
