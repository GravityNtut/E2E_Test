package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/cucumber/godog"
	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
	"gorm.io/driver/mysql"
	"gorm.io/driver/sqlserver"
	"gorm.io/gorm"
)

func TestFeatures(t *testing.T) {
	suite := godog.TestSuite{
		ScenarioInitializer: InitializeScenario,
		Options: &godog.Options{
			Format:        "pretty",
			Paths:         []string{"./"},
			StopOnFailure: true,
			TestingT:      t,
		},
	}
	if suite.Run() != 0 {
		t.Fatal("non-zero status returned, failed to run feature tests")
	}
}

var config struct {
	SourceDB serverInfo `json:"source-mssql"`
	TargetDB serverInfo `json:"target-mysql"`
}

type serverInfo struct {
	Type     string `json:"type"`
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Username string `json:"username"`
	Password string `json:"password"`
	Database string `json:"database"`
}

// Table Account schema
type Account struct {
	ID    int
	Name  string `gorm:"size:50"`
	Phone string `gorm:"size:16"`
}

var Cmd *exec.Cmd

func getContainerStateByName(ctName string) (*types.ContainerState, error) {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, err
	}

	containerInfo, err := cli.ContainerInspect(context.Background(), ctName)
	if err != nil {
		if client.IsErrNotFound(err) {
			return nil, fmt.Errorf("Container name %s is not found", ctName)
		}
		return nil, err
	}

	return containerInfo.State, nil
}

func connectToDB(s *serverInfo) (*gorm.DB, error) {
	if s.Type == "mysql" {
		dsn := fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?charset=utf8mb4&parseTime=True&loc=Local",
			s.Username, s.Password, s.Host, s.Port, s.Database)
		db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{})
		if err != nil {
			return nil, fmt.Errorf("Failed to connect to database: %v", err)
		}
		return db, nil
	} else if s.Type == "mssql" {
		dsn := fmt.Sprintf("sqlserver://%s:%s@%s:%d?database=%s",
			s.Username, s.Password, s.Host, s.Port, s.Database)
		db, err := gorm.Open(sqlserver.Open(dsn), &gorm.Config{})
		if err != nil {
			return nil, fmt.Errorf("Failed to connect to database: %v", err)
		}
		return db, nil
	} else {
		return nil, fmt.Errorf("Invalid database type '%s'", s.Type)
	}
}

func LoadConfig() error {
	str, err := os.ReadFile("./config.json")
	if err != nil {
		return err
	}
	err = json.Unmarshal(str, &config)
	if err != nil {
		return err
	}
	return nil
}

func CreateTestDB(dialector gorm.Dialector, createTestDBFilePath string) bool {
	db, err := gorm.Open(dialector, &gorm.Config{})
	if err != nil {
		fmt.Printf("正在嘗試連接至 %s Database", dialector.Name())
		return false
	}

	sqlDB, err := db.DB()
	if err != nil {
		fmt.Printf("Failed to get sql.DB from gorm.DB: %v", err)
		return false
	}

	if err := sqlDB.Ping(); err != nil {
		fmt.Printf("正在等待 %s Database 可用", dialector.Name())
		return false
	}
	str, err := os.ReadFile(createTestDBFilePath)
	if err != nil {
		fmt.Println("Failed to read create_testDB.sql: ", err)
		return false
	}
	db.Exec(string(str))
	return true
}

func InitAccountTable(s *serverInfo, createTableFilePath string) error {
	var err error
	sourceDB, err := connectToDB(s)
	if err != nil {
		return fmt.Errorf("Failed to connect to '%s' database: %v", s.Type, err)
	}

	db, err := sourceDB.DB()
	if err != nil {
		return fmt.Errorf("Failed to connect to '%s' database: %v", s.Type, err)
	}
	str, err := os.ReadFile(createTableFilePath)
	if err != nil {
		return fmt.Errorf("Failed to read create_table.sql: %v", err)
	}
	if _, err := db.Exec(string(str)); err != nil {
		return fmt.Errorf("Failed to create table: %v", err)
	}
	return nil
}

func DBServerInit(dbStr string) error {
	if err := LoadConfig(); err != nil {
		return fmt.Errorf("Failed to load config: %v", err)
	}
	var (
		dialector            gorm.Dialector
		createTestDBFilePath string
		serverInfo           *serverInfo
		createTableFilePath  string
	)

	switch dbStr {
	case "source-mssql":
		info := &config.SourceDB
		dsn := fmt.Sprintf("sqlserver://%s:%s@%s:%d",
			info.Username, info.Password, info.Host, info.Port)

		dialector = sqlserver.Open(dsn)
		createTestDBFilePath = "./assets/mssql/create_testDB.sql"
		serverInfo = &config.SourceDB
		createTableFilePath = "./assets/mssql/create_table.sql"
	case "target-mysql":
		info := &config.TargetDB
		dsn := fmt.Sprintf("%s:%s@tcp(%s:%d)/?charset=utf8mb4&parseTime=True&loc=Local",
			info.Username, info.Password, info.Host, info.Port)

		dialector = mysql.Open(dsn)
		createTestDBFilePath = "./assets/mysql/create_testDB.sql"

		serverInfo = &config.TargetDB
		createTableFilePath = "./assets/mysql/create_table.sql"
	default:
		return fmt.Errorf("Invalid database type '%s'", dbStr)
	}

	for !CreateTestDB(dialector, createTestDBFilePath) {
		time.Sleep(5 * time.Second)
	}
	if err := InitAccountTable(serverInfo, createTableFilePath); err != nil {
		return err
	}
	return nil
}

func dockerComposeServiceStart(serviceName string) error {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return fmt.Errorf("Failed to create docker client: %v", err)
	}
	if err := cli.ContainerStart(context.Background(), serviceName, container.StartOptions{}); err != nil {
		return fmt.Errorf("Failed to start container '%s': %v", serviceName, err)
	}
	return nil
}

func InitializeScenario(ctx *godog.ScenarioContext) {
	ctx.Given(`^啟動 "([^"]*)" 服務$`, dockerComposeServiceStart)
	ctx.Given(`^初始化 "([^"]*)" 資料表 Accounts$`, DBServerInit)
	// ctx.Given(`^開啟 "([^"]*)" CDC 設定$`, dbServerInfoSetup)
	// ctx.Given(`^創建 Data Product "([^"]*)"$`, dbServerInfoSetup)

	// // ctx.Step(`^container "([^"]*)" and process "([^"]*)" ready \(timeout "(\d+)"\)$`, containerAndProcessReadyTimeoutSeconds)
	// ctx.Step(`^container "([^"]*)" was "([^"]*)" \(timeout "(\d+)"\)$`, containerStateWasTimeoutSeconds)
	// ctx.Step(`^測試資料庫 "([^"]*)" 連線資訊:$`, dbServerInfoSetup)
	// // ctx.Step(`^docker-compose "([^"]*)" service "([^"]*)" \(in "([^"]*)"\)$`, dockerComposeServiceIn)
	// ctx.Step(`^"([^"]*)" 資料表 "([^"]*)" 筆數為 "(\d+)" \(timeout "([^"]*)"\)$`, verifyRowCountTimeoutSeconds)
	// ctx.Step(`^"([^"]*)" 資料表 "([^"]*)" 筆數大於 "(\d+)" \(timeout "([^"]*)"\)$`, rowCountLargeThenTimeoutSeconds)
	// ctx.Step(`^"([^"]*)" 資料表 "([^"]*)" 筆數小於 "(\d+)" \(timeout "([^"]*)"\)$`, rowCountLessThenTimeoutSeconds)
	// ctx.Step(`^"([^"]*)" 資料表 "([^"]*)" 新增 "([^"]*)" 筆 \(ID 開始編號 "(\d+)"\)$`, insertDummyDataFromID)
	// ctx.Step(`^"([^"]*)" 資料表 "([^"]*)" 清空$`, cleanUpTable)
	// ctx.Step(`^"([^"]*)" 資料表 "([^"]*)" 更新 "([^"]*)" 筆 - 每筆 Name 的內容加上後綴 updated \(ID 開始編號 "(\d+)"\)$`, updateRowDummyDataFromID)
	// ctx.Step(`^"([^"]*)" 資料表 "([^"]*)" 有與 "([^"]*)" 一致的資料筆數與內容 \(timeout "([^"]*)"\)$`, verifyFromToRowCountAndContentTimeoutSeconds)
	// ctx.Step(`^"([^"]*)" ready, "([^"]*)" flag file existed \(timeout "([^"]*)"\)$`, serviceReady)
}
