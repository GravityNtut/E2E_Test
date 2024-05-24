Feature: Gravity2 MSSQL to MySQL - 無資料異動時 - 元件重啟
    Scenario: 異動同步後重啟元件，等待元件啟動完成，接著新增/更新/刪除資料
        # Given 啟動 "source-mssql" 服務
        # Given 初始化 "source-mssql" 資料表 Accounts
        # Given 啟動 "target-mysql" 服務
        # Given 初始化 "target-mysql" 資料表 Accounts
        Given 啟動 "nats-jetstream" 服務
        Given 啟動 "gravity-dispatcher" 服務
        Given 創建 Data Product "Accounts"
        Given 啟動 "atomic" 服務
        Given 初始化 "atomic" 服務
        # Given 啟動 "gravity-adapter-mssql" 服務
        
        # Then "source-mssql" 資料表 "Accounts" 筆數為 "0" (timeout "3")
        # Given "source-mssql" 資料表 "Accounts" 新增 "1000" 筆 (ID 開始編號 "1")
        # Then "target-mysql" 資料表 "Accounts" 有與 "source-mssql" 一致的資料筆數與內容 (timeout "90")
        # Given docker-compose "stop" service "<RestartService>" (in "foreground")
        # Then container "<RestartService>" was "exited" (timeout "120")
        # Given docker-compose "start" service "<RestartService>" (in "foreground")
        # When container "<RestartService>" and process "<RestartService>" ready (timeout "120")
        # Given "source-mssql" 資料表 "Accounts" 更新 "1000" 筆 - 每筆 Name 的內容加上後綴 updated (ID 開始編號 "1")
        # Given "source-mssql" 資料表 "Accounts" 新增 "1000" 筆 (ID 開始編號 "1001")
        # Then "target-mysql" 資料表 "Accounts" 有與 "source-mssql" 一致的資料筆數與內容 (timeout "90")
        # Given "source-mssql" 資料表 "Accounts" 清空
        # Then "target-mysql" 資料表 "Accounts" 筆數為 "0" (timeout "120")

        Examples:
            |   RestartService      | 
            | gravity-adapter-mssql | 
            # | gravity-dispatcher    |
            # | nats-jetstream        |
            # | atomic                |
            # | source-mssql          |
            # | target-mysql          |