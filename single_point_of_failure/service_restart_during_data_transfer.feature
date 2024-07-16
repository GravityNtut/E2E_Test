Feature: Gravity2 MSSQL to MySQL - Service restart during data transfer
    Background: 初始化測試環境
        Given 創建所有服務
        Given 讀取初始設定檔
        Given 啟動 "source-mssql" 服務 (timeout "60")
        Given 初始化 "source-mssql" 資料表 Accounts
        Given 啟動 "target-mysql" 服務 (timeout "60")
        Given 初始化 "target-mysql" 資料表 Accounts
        Given 啟動 "nats-jetstream" 服務 (timeout "60")
        Given 啟動 "gravity-dispatcher" 服務 (timeout "60")
        Given 創建 Data Product Accounts
        Given 設置 atomic flow 文件
        Given 啟動 "atomic" 服務 (timeout "60")
        Given 啟動 "gravity-adapter-mssql" 服務 (timeout "60")
        
    Scenario Outline: 資料傳輸期間重啟元件，等待元件啟動完成，接著新增/更新/刪除資料
        Then "source-mssql" 資料表 "Accounts" 筆數為 "0" (timeout "3")
        Given "source-mssql" 資料表 "Accounts" 開始持續新增 "3000" 筆 (ID 開始編號 "1")
        Given docker compose "stop" service "<RestartService>" (in "foreground")
        Then container "<RestartService>" was "exited" (timeout "120")
        # Then Wait "20" seconds
        Given docker compose "start" service "<RestartService>" (in "foreground")
        When container "<RestartService>" ready (timeout "120")
        Then 等待 "source-mssql" 資料表 "Accounts" 新增完成 (timeout "120")
        And "target-mysql" 資料表 "Accounts" 有與 "source-mssql" 一致的資料筆數與內容 (timeout "90")
        Given "source-mssql" 資料表 "Accounts" 開始持續更新 "3000" 筆 - 每筆 Name 的內容加上後綴 updated (ID 開始編號 "1") 並新增 "1000" 筆 (ID 開始編號 "3001")
        Given docker compose "stop" service "<RestartService>" (in "foreground")
        Then container "<RestartService>" was "exited" (timeout "120")
        # Then Wait "20" seconds
        Given docker compose "start" service "<RestartService>" (in "foreground")
        When container "<RestartService>" ready (timeout "120")
        Then 等待 "source-mssql" 資料表 "Accounts" 更新完成及新增完成 (timeout "120")
        And "target-mysql" 資料表 "Accounts" 有與 "source-mssql" 一致的資料筆數與內容 (timeout "90")
        Given "source-mssql" 資料表 "Accounts" 開始持續清空
        Given docker compose "stop" service "<RestartService>" (in "foreground")
        Then container "<RestartService>" was "exited" (timeout "120")
        # Then Wait "20" seconds
        Given docker compose "start" service "<RestartService>" (in "foreground")
        When container "<RestartService>" ready (timeout "120")
        Then 等待 "source-mssql" 資料表 "Accounts" 清空完成 (timeout "120")
        And "target-mysql" 資料表 "Accounts" 筆數為 "0" (timeout "120")
        
        Examples:
            |   RestartService      |
            | gravity-adapter-mssql |
            | gravity-dispatcher    | 
            | nats-jetstream        |
            | atomic                |
            # | source-mssql          |
            | target-mysql          |
