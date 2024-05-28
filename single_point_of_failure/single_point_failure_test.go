package e2e

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"testing"
	"time"

	log "github.com/sirupsen/logrus"

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

const (
	DockerComposeFile = "./docker-compose.yaml"
	SourceMSSQL       = "source-mssql"
	TargetMySQL       = "target-mysql"
	Dispatcher        = "gravity-dispatcher"
	Atomic            = "atomic"
	Adapter           = "gravity-adapter-mssql"
	NatsJetstream     = "nats-jetstream"
)

func CreateServices() error {
	cmd := exec.Command("docker", "compose", "-f", DockerComposeFile, "create")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		log.Fatal(err)
	}
	return nil
}

func CloseAllServices() error {
	cmd := exec.Command("docker", "compose", "-f", DockerComposeFile, "down")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		log.Fatal(err)
	}
	return nil
}

func GetContainerStateByName(ctName string) (*types.ContainerState, error) {
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

func ConnectToDB(s *serverInfo) (*gorm.DB, error) {
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
	}
	return nil, fmt.Errorf("Invalid database type '%s'", s.Type)
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

func CreateTestDB(dialector gorm.Dialector, createTestDBFilePath string) error {
	db, err := DatabaseLifeCheck(dialector, 60)
	if err != nil {
		return err
	}
	str, err := os.ReadFile(createTestDBFilePath)
	if err != nil {
		return fmt.Errorf("Failed to read create_testDB.sql: %v", err)
	}
	db.Exec(string(str))
	return nil
}

func InitAccountTable(s *serverInfo, createTableFilePath string) error {
	var err error
	sourceDB, err := ConnectToDB(s)
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
	var (
		dialector            gorm.Dialector
		createTestDBFilePath string
		serverInfo           *serverInfo
		createTableFilePath  string
	)

	switch dbStr {
	case SourceMSSQL:
		info := &config.SourceDB
		dsn := fmt.Sprintf("sqlserver://%s:%s@%s:%d",
			info.Username, info.Password, info.Host, info.Port)

		dialector = sqlserver.Open(dsn)
		createTestDBFilePath = "./assets/mssql/create_testDB.sql"
		serverInfo = &config.SourceDB
		createTableFilePath = "./assets/mssql/create_table.sql"
	case TargetMySQL:
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

	if err := CreateTestDB(dialector, createTestDBFilePath); err != nil {
		return err
	}
	if err := InitAccountTable(serverInfo, createTableFilePath); err != nil {
		return err
	}
	return nil
}

func DockerComposeServiceStart(serviceName string, timeout int) error {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return fmt.Errorf("Failed to create docker client: %v", err)
	}
	if err := cli.ContainerStart(context.Background(), serviceName, container.StartOptions{}); err != nil {
		return fmt.Errorf("Failed to start container '%s': %v", serviceName, err)
	}
	return ContainerAndProcessReadyTimeoutSeconds(serviceName, timeout)
}

func DatabaseLifeCheck(dialector gorm.Dialector, timeout int) (*gorm.DB, error) {
	for i := 0; i < timeout; i += 5 {
		db, err := gorm.Open(dialector, &gorm.Config{})
		if err != nil {
			log.Infof("正在嘗試連接至 %s Database (%d sec)", dialector.Name(), i)
			time.Sleep(5 * time.Second)
			continue
		}

		sqlDB, err := db.DB()
		if err != nil {
			log.Infof("Failed to get sql.DB from gorm.DB: %v", err)
			time.Sleep(5 * time.Second)
			continue
		}

		if err := sqlDB.Ping(); err != nil {
			log.Infof("正在等待 %s Database 可用 (%d sec)", dialector.Name(), i)
			time.Sleep(5 * time.Second)
			continue
		}
		return db, nil
	}
	return nil, fmt.Errorf("連接至 %s Database 逾時", dialector.Name())
}

func NatsLifeCheck(timeout int) (*nats.Conn, error) {
	for i := 0; i < timeout; i++ {
		nc, err := nats.Connect("nats://127.0.0.1:32803")
		if err != nil {
			log.Infoln("無法連接至 NATS 伺服器，等待 1 秒後重試")
			time.Sleep(1 * time.Second)
			continue
		}
		return nc, nil
	}
	return nil, fmt.Errorf("連接至 NATS 伺服器逾時")
}

func ContainerLifeCheck(ctName, psName string, timeout int) error {
	for i := 0; i < timeout; i++ {
		err := CheckProcessRunningInContainer(ctName, psName)
		if err == nil {
			log.Infof("container '%s' is ready.. %d", ctName, i)
			return nil
		}
		if i%10 == 0 {
			log.Infof("Waiting for container '%s' to be ready.. (%d sec)", ctName, i)
		}
		time.Sleep(1 * time.Second)
	}
	return fmt.Errorf("服務 '%s' 在 %d秒 後逾時未啟動", ctName, timeout)

}

func CreateDataProduct() error {
	nc, err := nats.Connect("nats://127.0.0.1:32803")
	if err != nil {
		return err
	}
	defer nc.Close()
	containerID := Dispatcher

	cmd := []string{"sh", "/assets/dispatcher/create_product.sh"}
	result, err := ExecuteContainerCommand(containerID, cmd)
	if err != nil {
		return err
	}
	log.Infoln(result)
	return nil
}

func GetDBInstance(loc string) (*gorm.DB, error) {
	switch loc {
	case SourceMSSQL:
		return ConnectToDB(&config.SourceDB)
	case TargetMySQL:
		return ConnectToDB(&config.TargetDB)
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
		log.Infof("Waiting for '%s' table '%s' to has %d records.. (%d sec), current total: %d",
			loc, tableName, expectedRowCount, retry, currRowCount)
		time.Sleep(1 * time.Second)
	}

	return fmt.Errorf("Expected %d records, but got %d", expectedRowCount, currRowCount)
}

func InsertDummyDataFromID(loc, tableName string, total int, beginID int) error {
	db, err := GetDBInstance(loc)
	if err != nil {
		return err
	}
	log.Infof("Inserting total %d records to '%s' - '%s', begin ID '%d'",
		total, loc, tableName, beginID)
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
			log.Printf("Failed to insert '%d th' record: %v", i, result.Error)
			insFailed++
		}
	}

	elapsed := time.Since(start)
	log.Infof("Inserted total %d records to '%s', ID '%d ~ %d' (elapsed: %s)",
		total, loc, beginID, beginID+total-1, elapsed)
	return nil
}

func CompareRecords(sourceDB, targetDB *gorm.DB) (int, error) {
	var (
		limit    = 5000
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
		log.Infof("Waiting for '%s' table '%s' to has %d records.. (%d sec), current total: %d",
			locTo, tableName, srcRowCount, retry, targetRowCount)
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
		log.Infof("Waiting for '%s' table '%s' to has %d same content.. (%d sec), last match ID %d",
			locTo, tableName, srcRowCount, retry, lastMatchID)
		time.Sleep(1 * time.Second)
	}
	return fmt.Errorf("content of table '%s' is not the same after %d second", tableName, timeoutSec)
}

func ExecuteContainerCommand(containerID string, cmd []string) (string, error) {
	ctx := context.Background()

	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return "", fmt.Errorf("Error creating Docker client: %v", err)
	}

	execConfig := types.ExecConfig{
		Cmd:          cmd,
		AttachStdout: true,
		AttachStderr: true,
	}

	execIDResp, err := cli.ContainerExecCreate(ctx, containerID, execConfig)
	if err != nil {
		return "", fmt.Errorf("error creating exec instance: %v", err)

	}

	resp, err := cli.ContainerExecAttach(ctx, execIDResp.ID, types.ExecStartCheck{})
	if err != nil {
		return "", fmt.Errorf("Error attaching to exec instance: %v", err)
	}
	defer resp.Close()

	scanner := bufio.NewScanner(resp.Reader)
	result := ""
	for scanner.Scan() {
		result += scanner.Text() + "\n"
	}
	return result, nil
}

func GetToken() (string, error) {
	cmdString := []string{"/gravity-cli", "token", "create", "-s", "nats-jetstream:32803"}
	result, err := ExecuteContainerCommand(Dispatcher, cmdString)
	if err != nil {
		return "", err
	}
	regexp := regexp.MustCompile(`Token: (.*)`)
	parts := regexp.FindStringSubmatch(result)
	if parts == nil {
		return "", fmt.Errorf("Failed to get token from result: %s", result)
	}
	return parts[1], nil
}

func InitAtomicService() error {
	token, err := GetToken()
	if err != nil {
		return err
	}
	// 更新 unprocessed_cred.json 的 accessToken 資料並輸出到 unencrypted_cred.json
	inputFileName := "unprocessed_cred.json"
	byteValue, err := os.ReadFile(inputFileName)
	if err != nil {
		return fmt.Errorf("Failed to read JSON file: %v", err)
	}

	var data map[string]map[string]string
	if err := json.Unmarshal(byteValue, &data); err != nil {
		return fmt.Errorf("Failed to parse JSON file: %v", err)
	}

	for _, component := range data {
		if _, exist := component["accessToken"]; exist {
			component["accessToken"] = token
		}
	}

	outputFileName := "tmp/unencrypted_cred.json"
	modifiedFile, err := os.Create(outputFileName)
	if err != nil {
		return fmt.Errorf("Failed to create output JSON file: %v", err)
	}

	defer func() {
		if err := modifiedFile.Close(); err != nil {
			log.Fatal(err)
		}
	}()

	modifiedJSON, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return fmt.Errorf("Failed to marshal modified JSON: %v", err)
	}

	if _, err := modifiedFile.Write(modifiedJSON); err != nil {
		return fmt.Errorf("Failed to write to %s JSON file: %v", outputFileName, err)
	}

	// 執行 flowEnc.sh 加密 unencrypted_cred.json 並將輸出導向 flows_cred.json
	cmd := exec.Command("sh", "./flowEnc.sh", outputFileName,
		"./assets/atomic", ">", "./assets/atomic/flows_cred.json")
	credFile, err := os.Create("./assets/atomic/flows_cred.json")
	if err != nil {
		return fmt.Errorf("Failed to create flows_cred.json: %v", err)
	}
	var stderr bytes.Buffer
	cmd.Stdout = credFile
	cmd.Stderr = &stderr
	err = cmd.Run()
	if err != nil {
		return fmt.Errorf("Failed to execute flowEnc.sh: %s", stderr.String())
	}
	return nil
}

func DockerComposeServiceIn(action, serviceName, executionMode string) error {
	if action != "start" && action != "stop" && action != "restart" {
		return fmt.Errorf("Invalid docker-compose action '%s'", action)
	}

	if executionMode != "foreground" && executionMode != "background" {
		return fmt.Errorf("Invalid docker compose execution mode '%s'", executionMode)
	}

	cmd := exec.Command("docker", "compose", "-f", DockerComposeFile, action, serviceName)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	switch executionMode {
	case "foreground":
		// 在前景執行命令, 等待命令執行結束
		if err := cmd.Run(); err != nil {
			log.Fatal(err)
		}
	case "background":
		// 在背景執行命令，不等待命令執行結束
		if err := cmd.Start(); err != nil {
			log.Fatal(err)
		}
	}
	return nil
}

func ContainerStateWasTimeoutSeconds(ctName string, expectedState string, timeoutSec int) error {
	var (
		err   error
		i     int
		state *types.ContainerState
	)

	for i = 0; i < timeoutSec; i++ {
		state, err = GetContainerStateByName(ctName)
		if err == nil {
			log.Debugf("Container '%s' state: %s", ctName, state.Status)
			if expectedState == "exited" && state.Running == false {
				return nil
			}
			if expectedState == "running" && state.Running == true {
				return nil
			}
		} else {
			log.Errorf("Failed to get container '%s' state: %s", ctName, err.Error())
		}
		if i%10 == 0 {
			log.Infof("Waiting for container '%s' state to be '%s'.. (%d sec)", ctName, expectedState, i)
		}
		time.Sleep(1 * time.Second)
	}

	if state != nil {
		log.Errorf("Container '%s' state expected '%s', but got '%s' after %d sec", ctName, expectedState, state.Status, i)
	} else {
		log.Errorf("Container '%s' state expected '%s', but not found after %d sec", ctName, expectedState, i)
	}
	return err
}

func CheckProcessRunningInContainer(containerName, processName string) error {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return err
	}

	containers, err := cli.ContainerList(context.Background(), container.ListOptions{})
	if err != nil {
		return err
	}

	for _, container := range containers {
		if container.Names[0] == "/"+containerName {
			processes, err := cli.ContainerTop(context.Background(), container.ID, []string{})
			if err != nil {
				return err
			}
			for _, processInfo := range processes.Processes {
				cmdLine := processInfo[7] // Top 看到的一行資訊，第 8 個欄位是執行的 command line
				// log.Infof("command line: %s", cmdLine)
				if cmdLine == "/"+processName || cmdLine == processName {
					return nil
				}

				if len(cmdLine) > 0 {
					cmds := strings.Split(cmdLine, " ")
					if len(cmds) > 0 {
						// log.Infof("cmds name: %v", cmds)
						for _, cmd := range cmds {
							// log.Debugf("cmd: %s", cmd)
							if cmd == processName || cmd == "/"+processName {
								return nil
							}
						}
					}
				}
			}
		}
	}

	return fmt.Errorf("Process %s is not running in container %s", processName, containerName)
}

func ContainerAndProcessReadyTimeoutSeconds(ctName string, timeoutSec int) error {
	//wait gravity-adapter-mssql process ready, timeout 60s
	switch ctName {
	case Atomic:
		return ContainerLifeCheck(ctName, "node-red", timeoutSec)
	case Adapter:
		fallthrough
	case Dispatcher:
		return ContainerLifeCheck(ctName, ctName, timeoutSec)
	case NatsJetstream:
		nc, err := NatsLifeCheck(timeoutSec)
		if err != nil {
			return err
		}
		defer nc.Close()
		return nil
	case TargetMySQL:
		s := config.TargetDB
		dsn := fmt.Sprintf("%s:%s@tcp(%s:%d)/?charset=utf8mb4&parseTime=True&loc=Local",
			s.Username, s.Password, s.Host, s.Port)
		db := mysql.Open(dsn)
		_, err := DatabaseLifeCheck(db, timeoutSec)
		if err != nil {
			return err
		}
		return nil
	case SourceMSSQL:
		s := config.SourceDB
		dsn := fmt.Sprintf("sqlserver://%s:%s@%s:%d",
			s.Username, s.Password, s.Host, s.Port)
		db := sqlserver.Open(dsn)
		_, err := DatabaseLifeCheck(db, timeoutSec)
		if err != nil {
			return err
		}
		return nil
	default:
		return fmt.Errorf("Invalid container name '%s'", ctName)
	}
}

func UpdateRowDummyDataFromID(loc, tableName string, total, beginID int) error {
	db, err := GetDBInstance(loc)
	if err != nil {
		return err
	}

	log.Infof("Updating %d records to '%s' - '%s'", total, loc, tableName)
	// Update dummy data
	opFailed := 0
	start := time.Now()

	for i := beginID; i < total+beginID; i++ {
		// 更新 Name 欄位
		err = db.Table("Accounts").Where("ID = ?", i).Update("Name", gorm.Expr("CONCAT(Name, ?)", " updated")).Error
		if err != nil {
			log.Errorf("Failed to insert '%d th' record: %v", i, err)
			opFailed++
		}
	}

	elapsed := time.Since(start)
	log.Infof("Updated total %d records to '%s' table '%s', ID '%d ~ %d', failed count %d (elapsed: %s)",
		total, loc, tableName, beginID, beginID+total-1, opFailed, elapsed)
	return nil
}

func CleanUpTable(loc, tableName string) error {
	db, err := GetDBInstance(loc)
	if err != nil {
		return err
	}
	// Clean up
	result := db.Exec(fmt.Sprintf("DELETE FROM %s", tableName))
	if result.Error != nil {
		return fmt.Errorf("Failed to exec clean up '%s' table '%s': %v", loc, tableName, result.Error)
	}

	var rowCount int64
	db.Table(tableName).Count(&rowCount)
	if rowCount != 0 {
		return fmt.Errorf("Failed to clean up '%s' table '%s'", loc, tableName)
	}
	return nil
}

func InitializeScenario(ctx *godog.ScenarioContext) {
	ctx.Given(`^創建所有服務$`, CreateServices)
	ctx.Given(`^刪除所有服務$`, CloseAllServices)
	ctx.Given(`^讀取初始設定檔$`, LoadConfig)
	ctx.Given(`^啟動 "([^"]*)" 服務 \(timeout "(\d+)"\)$`, DockerComposeServiceStart)
	ctx.Given(`^初始化 "([^"]*)" 資料表 Accounts$`, DBServerInit)
	ctx.Given(`^創建 Data Product Accounts$`, CreateDataProduct)
	ctx.Given(`^設置 atomic flow 文件$`, InitAtomicService)

	ctx.Then(`^"([^"]*)" 資料表 "([^"]*)" 筆數為 "(\d+)" \(timeout "([^"]*)"\)$`, VerifyRowCountTimeoutSeconds)
	ctx.Given(`^"([^"]*)" 資料表 "([^"]*)" 新增 "([^"]*)" 筆 \(ID 開始編號 "(\d+)"\)$`, InsertDummyDataFromID)
	ctx.Then(`^"([^"]*)" 資料表 "([^"]*)" 有與 "([^"]*)" 一致的資料筆數與內容 \(timeout "([^"]*)"\)$`, VerifyFromToRowCountAndContentTimeoutSeconds)
	ctx.Given(`^docker compose "([^"]*)" service "([^"]*)" \(in "([^"]*)"\)$`, DockerComposeServiceIn)
	ctx.Then(`^container "([^"]*)" was "([^"]*)" \(timeout "(\d+)"\)$`, ContainerStateWasTimeoutSeconds)
	ctx.When(`^container "([^"]*)" ready \(timeout "(\d+)"\)$`, ContainerAndProcessReadyTimeoutSeconds)

	ctx.Given(`^"([^"]*)" 資料表 "([^"]*)" 更新 "([^"]*)" 筆 - 每筆 Name 的內容加上後綴 updated \(ID 開始編號 "(\d+)"\)$`, UpdateRowDummyDataFromID)
	ctx.Given(`^"([^"]*)" 資料表 "([^"]*)" 清空$`, CleanUpTable)
}
