package e2e

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"testing"
	"time"

	"github.com/cucumber/godog"
	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
	"github.com/nats-io/nats.go"
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

func DockerComposeServiceStart(serviceName string) error {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return fmt.Errorf("Failed to create docker client: %v", err)
	}
	if err := cli.ContainerStart(context.Background(), serviceName, container.StartOptions{}); err != nil {
		return fmt.Errorf("Failed to start container '%s': %v", serviceName, err)
	}
	return nil
}

func CreateDataProduct(accounts string) error {
	var err error = fmt.Errorf("")
	var nc *nats.Conn
	for err != nil {
		nc, err = nats.Connect("nats://127.0.0.1:32803")
		if err != nil {
			fmt.Println("Failed to connect to NATS server, retrying...")
			time.Sleep(1 * time.Second)
		}
	}
	defer nc.Close()
	containerID := "gravity-dispatcher"

	cmd := []string{"sh", "/assets/dispatcher/create_product.sh"}
	result, err := ExecuteContainerCommand(containerID, cmd)
	if err != nil {
		return err
	}
	fmt.Println(result)
	return nil
}

func GetDBInstance(loc string) (*gorm.DB, error) {
	switch loc {
	case "source-mssql":
		return connectToDB(&config.SourceDB)
	case "target-mysql":
		return connectToDB(&config.TargetDB)
	default:
		return nil, fmt.Errorf("Invalid database location '%s'", loc)
	}

}

func VerifyRowCountTimeoutSeconds(loc, tableName string, expectedRowCount, timeoutSec int) error {
	db, err := GetDBInstance(loc)
	if err != nil {
		return err
	}

	var currRowCount int64
	var retry int
	for retry = 0; retry < timeoutSec; retry++ {
		db.Table(tableName).Count(&currRowCount)
		if currRowCount == int64(expectedRowCount) {
			return nil
		}
		fmt.Printf("Waiting for '%s' table '%s' to has %d records.. (%d sec), current total: %d\n",
			loc, tableName, expectedRowCount, retry, currRowCount)
		// log.Infof("Waiting for '%s' table '%s' to has %d records.. (%d sec), current total: %d",
		// 	loc, tableName, expectedRowCount, retry, currRowCount)
		time.Sleep(1 * time.Second)
	}

	return fmt.Errorf("Expected %d records, but got %d", expectedRowCount, currRowCount)
}

func InsertDummyDataFromID(loc, tableName string, total int, beginID int) error {
	db, err := GetDBInstance(loc)
	if err != nil {
		return err
	}

	fmt.Printf("Inserting total %d records to '%s' - '%s', begin ID '%d'\n",
		total, loc, tableName, beginID)

	// log.Infof("Inserting total %d records to '%s' - '%s', begin ID '%d'",
	// 	total, loc, tableName, beginID)
	// Insert dummy data from beginID
	insFailed := 0
	start := time.Now()

	for i := beginID; i < total+beginID; i++ {
		account := Account{
			ID:    i,
			Name:  fmt.Sprintf("Name %d", i),
			Phone: fmt.Sprintf("Phone %d", i),
		}
		query := fmt.Sprintf("INSERT INTO %s (id, name, phone) VALUES (%d, '%s', '%s')",
			tableName, account.ID, account.Name, account.Phone)
		result := db.Exec(query)
		if result.Error != nil {
			fmt.Printf("Failed to insert '%d th' record: %v", i, result.Error)
			// log.Printf("Failed to insert '%d th' record: %v", i, result.Error)
			insFailed++
		}
	}

	elapsed := time.Since(start)
	fmt.Printf("Inserted total %d records to '%s', ID '%d ~ %d' (elapsed: %s)",
		total, loc, beginID, beginID+total-1, elapsed)
	// log.Infof("Inserted total %d records to '%s', ID '%d ~ %d' (elapsed: %s)",
	// 	total, loc, beginID, beginID+total-1, elapsed)
	return nil
}

func CompareRecords(sourceDB, targetDB *gorm.DB) (int, error) {
	var (
		limit    = 1000
		offset   = 0
		moreData = true

		lastMatchID = 0
	)

	for moreData {
		var (
			records1 []Account
			records2 []Account
		)

		err := sourceDB.Table("Accounts").Order("id ASC").Limit(limit).Offset(offset).Find(&records1).Error
		if err != nil {
			return 0, fmt.Errorf("failed to retrieve records from source model: %v", err)
		}

		err = targetDB.Table("Accounts").Order("id ASC").Limit(limit).Offset(offset).Find(&records2).Error
		if err != nil {
			return 0, fmt.Errorf("failed to retrieve records from target model: %v", err)
		}

		for i := range records1 {
			if records1[i].Name != records2[i].Name {
				return lastMatchID, fmt.Errorf("ID: %d source has '%s', target has '%s'", records1[i].ID, records1[i].Name, records2[i].Name)
			}

			if records1[i].Phone != records2[i].Phone {
				return lastMatchID, fmt.Errorf("ID: %d source has '%s', target has '%s'", records1[i].ID, records1[i].Phone, records2[i].Phone)
			}
			lastMatchID = records1[i].ID
		}

		offset += limit

		if len(records1) < limit {
			moreData = false
		}
	}

	return lastMatchID, nil
}

func GetCount(db *gorm.DB, tableName string) (int64, error) {
	var count int64
	err := db.Table(tableName).Count(&count).Error
	return count, err
}

func VerifyFromToRowCountAndContentTimeoutSeconds(locTo, tableName, locFrom string, timeoutSec int) error {
	// compare source/target table row count
	sourceDB, err := GetDBInstance(locFrom)
	if err != nil {
		return err
	}
	targetDB, err := GetDBInstance(locTo)
	if err != nil {
		return err
	}
	srcRowCount, err := GetCount(sourceDB, tableName)
	if err != nil {
		return err
	}

	// Check number of records
	var retry int
	var targetRowCount int64
	for retry = 0; retry < timeoutSec; retry++ {
		targetRowCount, err := GetCount(targetDB, tableName)
		if err != nil {
			return err
		}
		fmt.Printf("Waiting for '%s' table '%s' to has %d records.. (%d sec), current total: %d\n",
			locTo, tableName, srcRowCount, retry, targetRowCount)
		// log.Infof("Waiting for '%s' table '%s' to has %d records.. (%d sec), current total: %d",
		// 	locTo, tableName, srcRowCount, retry, targetRowCount)
		if targetRowCount == srcRowCount {
			break
		}
		time.Sleep(1 * time.Second)
	}

	if retry+1 == timeoutSec {
		return fmt.Errorf("Number of records in table '%s' is %d, expected %d after %d second",
			tableName, targetRowCount, srcRowCount, timeoutSec)
	}

	for retry = 0; retry < timeoutSec; retry++ {
		lastMatchID, err := CompareRecords(sourceDB, targetDB)
		if err == nil {
			return nil
		}
		fmt.Printf("Waiting for '%s' table '%s' to has %d same content.. (%d sec), last match ID %d\n",
			locTo, tableName, srcRowCount, retry, lastMatchID)
		// log.Infof("Waiting for '%s' table '%s' to has %d same content.. (%d sec), last match ID %d",
		// 	locTo, tableName, srcRowCount, retry, lastMatchID)
		time.Sleep(1 * time.Second)
	}
	return fmt.Errorf("Content of table '%s' is not the same after %d second", tableName, timeoutSec)
}

func ExecuteContainerCommand(containerID string, cmd []string) (string, error) {
	ctx := context.Background()

	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return "", fmt.Errorf("Error creating Docker client: %v\n", err)
	}

	execConfig := types.ExecConfig{
		Cmd:          cmd,
		AttachStdout: true,
		AttachStderr: true,
	}

	execIDResp, err := cli.ContainerExecCreate(ctx, containerID, execConfig)
	if err != nil {
		return "", fmt.Errorf("Error creating exec instance: %v\n", err)

	}

	resp, err := cli.ContainerExecAttach(ctx, execIDResp.ID, types.ExecStartCheck{})
	if err != nil {
		return "", fmt.Errorf("Error attaching to exec instance: %v\n", err)
	}
	defer resp.Close()

	scanner := bufio.NewScanner(resp.Reader)
	result := ""
	for scanner.Scan() {
		result += scanner.Text() + "\n"
	}
	return result, nil
}

func InitAtomicService(serviceName string) error {
	cmdString := []string{"/gravity-cli", "token", "create", "-s", "nats-jetstream:32803"}
	result, err := ExecuteContainerCommand("gravity-dispatcher", cmdString)
	if err != nil {
		return err
	}
	regexp := regexp.MustCompile(`Token: (.*)`)
	parts := regexp.FindStringSubmatch(result)
	if parts == nil {
		return fmt.Errorf("Failed to get token from result: %s", result)
	}
	token := parts[1]
	fmt.Println("Token: ", token)
	return nil
}

func InitializeScenario(ctx *godog.ScenarioContext) {
	ctx.Given(`^啟動 "([^"]*)" 服務$`, DockerComposeServiceStart)
	ctx.Given(`^初始化 "([^"]*)" 資料表 Accounts$`, DBServerInit)
	ctx.Given(`^創建 Data Product "([^"]*)"$`, CreateDataProduct)
	ctx.Given(`^初始化 "([^"]*)" 服務$`, InitAtomicService)

	ctx.Step(`^"([^"]*)" 資料表 "([^"]*)" 筆數為 "(\d+)" \(timeout "([^"]*)"\)$`, VerifyRowCountTimeoutSeconds)
	ctx.Step(`^"([^"]*)" 資料表 "([^"]*)" 新增 "([^"]*)" 筆 \(ID 開始編號 "(\d+)"\)$`, InsertDummyDataFromID)
	// ctx.Step(`^"([^"]*)" 資料表 "([^"]*)" 有與 "([^"]*)" 一致的資料筆數與內容 \(timeout "([^"]*)"\)$`, VerifyFromToRowCountAndContentTimeoutSeconds)
	// // ctx.Step(`^container "([^"]*)" and process "([^"]*)" ready \(timeout "(\d+)"\)$`, containerAndProcessReadyTimeoutSeconds)
	// ctx.Step(`^container "([^"]*)" was "([^"]*)" \(timeout "(\d+)"\)$`, containerStateWasTimeoutSeconds)
	// ctx.Step(`^測試資料庫 "([^"]*)" 連線資訊:$`, dbServerInfoSetup)
	// // ctx.Step(`^docker-compose "([^"]*)" service "([^"]*)" \(in "([^"]*)"\)$`, dockerComposeServiceIn)
	// ctx.Step(`^"([^"]*)" 資料表 "([^"]*)" 筆數大於 "(\d+)" \(timeout "([^"]*)"\)$`, rowCountLargeThenTimeoutSeconds)
	// ctx.Step(`^"([^"]*)" 資料表 "([^"]*)" 筆數小於 "(\d+)" \(timeout "([^"]*)"\)$`, rowCountLessThenTimeoutSeconds)
	// ctx.Step(`^"([^"]*)" 資料表 "([^"]*)" 清空$`, cleanUpTable)
	// ctx.Step(`^"([^"]*)" 資料表 "([^"]*)" 更新 "([^"]*)" 筆 - 每筆 Name 的內容加上後綴 updated \(ID 開始編號 "(\d+)"\)$`, updateRowDummyDataFromID)
	// ctx.Step(`^"([^"]*)" ready, "([^"]*)" flag file existed \(timeout "([^"]*)"\)$`, serviceReady)
}
