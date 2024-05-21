package e2e

import (
	"context"
	"database/sql"
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
	log "github.com/sirupsen/logrus"
	"gorm.io/driver/mysql"
	"gorm.io/driver/sqlserver"
	"gorm.io/gorm"
)

// func init() {
// 	debugLevel := log.InfoLevel
// 	switch os.Getenv("E2E_DEBUG") {
// 	case log.TraceLevel.String():
// 		debugLevel = log.TraceLevel
// 	case log.DebugLevel.String():
// 		debugLevel = log.DebugLevel
// 	case log.ErrorLevel.String():
// 		debugLevel = log.ErrorLevel
// 	}

// 	log.SetFormatter(&log.TextFormatter{
// 		ForceColors:     true,
// 		FullTimestamp:   true,
// 		TimestampFormat: "2006-01-02 15:04:05+08:00",
// 	})
// 	log.SetLevel(debugLevel)
// 	log.SetOutput(os.Stdout)
// 	log.AddHook(NewLogrusCallerHook())
// 	log.Infof("Debug level is set to \"%s\"", debugLevel.String())
// }

// TestFeatures runs the e2e tests specified in any .features files in this directory
// * This test suite assumes that a LocalNet is running that can be accessed by `kubectl`
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

var (
	sourceInfo, targetInfo *serverInfo
	sourceDB, targetDB     *gorm.DB
)

// Table Account schema
type Account struct {
	ID    int
	Name  string `gorm:"size:50"`
	Phone string `gorm:"size:16"`
}

var Cmd *exec.Cmd

// func checkProcessRunningInContainer(containerName, processName string) error {
// 	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
// 	if err != nil {
// 		return err
// 	}

// 	containers, err := cli.ContainerList(context.Background(), types.ContainerListOptions{})
// 	if err != nil {
// 		return err
// 	}

// 	for _, container := range containers {
// 		if container.Names[0] == "/"+containerName {
// 			processes, err := cli.ContainerTop(context.Background(), container.ID, []string{})
// 			if err != nil {
// 				return err
// 			}
// 			for _, processInfo := range processes.Processes {
// 				cmdLine := processInfo[7] // Top 看到的一行資訊，第 8 個欄位是執行的 command line
// 				// log.Infof("command line: %s", cmdLine)
// 				if cmdLine == "/"+processName || cmdLine == processName {
// 					return nil
// 				}

// 				if len(cmdLine) > 0 {
// 					cmds := strings.Split(cmdLine, " ")
// 					if len(cmds) > 0 {
// 						// log.Infof("cmds name: %v", cmds)
// 						for _, cmd := range cmds {
// 							// log.Debugf("cmd: %s", cmd)
// 							if cmd == processName || cmd == "/"+processName {
// 								return nil
// 							}
// 						}
// 					}
// 				}
// 			}
// 		}
// 	}

// 	return fmt.Errorf("Process %s is not running in container %s", processName, containerName)
// }

// func containerAndProcessReadyTimeoutSeconds(ctName string, psName string, timeoutSec int) error {
// 	//wait gravity-adapter-mssql process ready, timeout 60s
// 	var err error
// 	var i int
// 	for i = 0; i < timeoutSec; i++ {
// 		err = checkProcessRunningInContainer(ctName, psName)
// 		if err == nil {
// 			break
// 		}
// 		if i%10 == 0 {
// 			log.Infof("Waiting for container '%s' to be ready.. (%d sec)", ctName, i)
// 		}
// 		time.Sleep(1 * time.Second)
// 	}
// 	log.Infof("container '%s' is ready.. %d", ctName, i)
// 	return err
// }

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

func containerStateWasTimeoutSeconds(ctName string, expectedState string, timeoutSec int) error {
	var (
		err   error
		i     int
		state *types.ContainerState
	)

	for i = 0; i < timeoutSec; i++ {
		state, err = getContainerStateByName(ctName)
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

func checkMSSQLServer(dsn string) bool {
    db, err := sql.Open("sqlserver", dsn)
    if err != nil {
		fmt.Println(err)
        return false
    }
    defer db.Close()

    if err := db.Ping() ; err != nil {
		fmt.Println(err)
        return false
    }
    return true
}

func dbServerInit(dbStr string) error {
	if err := LoadConfig(); err != nil {
		return fmt.Errorf("Failed to load config: %v", err)
	}
	s := &config.SourceDB
	dsn := fmt.Sprintf("sqlserver://%s:%s@%s:%d?database=%s",
			s.Username, s.Password, s.Host, s.Port, s.Database)
	
	for!checkMSSQLServer(dsn) {
		time.Sleep(1 * time.Second)
	}
	var err error
	switch dbStr {
	case "source-mssql":
		sourceInfo = &config.SourceDB
		sourceDB, err = connectToDB(sourceInfo)
		if err != nil {
			return fmt.Errorf("Failed to connect to '%s' database: %v", dbStr, err)
		}
	case "target-mysql":
		targetInfo = &config.TargetDB
		targetDB, err = connectToDB(targetInfo)
		if err != nil {
			return fmt.Errorf("Failed to connect to '%s' database: %v", dbStr, err)
		}
	default:
		return fmt.Errorf("Invalid DB '%s'", dbStr)
	}

	db, err := getDBInstance(dbStr)
	if err != nil {
		return fmt.Errorf("Failed to connect to '%s' database: %v", dbStr, err)
	}
	switch dbStr {
	case "source-mssql":
		str, err := os.ReadFile("./assets/mssql/create_source.sql")
		if err != nil {
			return fmt.Errorf("Failed to read create_source.sql: %v", err)
		}
		db.Exec(string(str))
	case "target-mysql":
		str, err := os.ReadFile("./assets/mysql/create_target.sql")
		if err != nil {
			return fmt.Errorf("Failed to read create_target.sql: %v", err)
		}
		db.Exec(string(str))
	default:
		return fmt.Errorf("Invalid DB '%s'", dbStr)
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

// func dockerComposeServiceIn(action, serviceName, executionMode string) error {
// 	if action != "start" && action != "stop" && action != "restart" {
// 		return fmt.Errorf("Invalid docker-compose action '%s'", action)
// 	}

// 	if executionMode != "foreground" && executionMode != "background" {
// 		return fmt.Errorf("Invalid docker-compose execution mode '%s'", executionMode)
// 	}

// 	cmd := exec.Command("docker-compose", "-f", "./docker-compose.yaml", action, serviceName)
// 	cmd.Stdout = os.Stdout
// 	cmd.Stderr = os.Stderr

// 	switch executionMode {
// 	case "foreground":
// 		// 在前景執行命令, 等待命令執行結束
// 		if err := cmd.Run(); err != nil {
// 			log.Fatal(err)
// 		}
// 	case "background":
// 		// 在背景執行命令，不等待命令執行結束
// 		if err := cmd.Start(); err != nil {
// 			log.Fatal(err)
// 		}
// 	}
// 	return nil
// }

func getDBInstance(loc string) (*gorm.DB, error) {
	var (
		gormDB *gorm.DB
		sqlDB  *sql.DB
		err    error
	)

	switch loc {
	case "source-mssql":
		if sourceDB == nil {
			return nil, fmt.Errorf("Source database is not setup")
		}
		gormDB = sourceDB
		sqlDB, err = sourceDB.DB()
		if err != nil {
			return nil, fmt.Errorf("Failed to get sourceDB instance: %v", err)
		}
	case "target-mysql":
		if targetDB == nil {
			return nil, fmt.Errorf("Source database is not setup")
		}
		gormDB = targetDB
		sqlDB, err = targetDB.DB()
		if err != nil {
			return nil, fmt.Errorf("Failed to get sourceDB instance: %v", err)
		}
	default:
		return nil, fmt.Errorf("Invalid location '%s'", loc)
	}

	// check if the connection is still alive
	if err := sqlDB.Ping(); err != nil {
		log.Infof("Lost connection to '%s' database: %v", loc, err)
		if err := sqlDB.Close(); err != nil {
			return nil, fmt.Errorf("Failed to close sourceDB connection: %v", err)
		}
		log.Infof("Reconnecting to '%s' database", loc)
		var retryCount int
		var newGormDB *gorm.DB
		for retryCount < 30 {
			newGormDB, err = connectToDB(sourceInfo)
			if err == nil {
				break
			}
			retryCount++
			if retryCount == 120 {
				return nil, fmt.Errorf("Failed to reconnect to 'source' database after %d seconds: %v", retryCount, err)
			}
			log.Debugf("Retrying to connect to '%s' database..", loc)
			time.Sleep(1 * time.Second)
		}
		log.Debugf("Reconnected to '%s' database", loc)
		switch loc {
		case "source":
			sourceDB = newGormDB
		case "target":
			targetDB = newGormDB
		}
		gormDB = newGormDB
	}

	return gormDB, nil
}

func verifyRowCountTimeoutSeconds(loc, tableName string, expectedRowCount, timeoutSec int) error {
	db, err := getDBInstance(loc)
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

func rowCountLargeThenTimeoutSeconds(loc, tableName string, expectedRowCount, timeoutSec int) error {
	db, err := getDBInstance(loc)
	if err != nil {
		return err
	}

	var currRowCount int64
	var retry int
	for retry = 0; retry < timeoutSec; retry++ {
		db.Table(tableName).Count(&currRowCount)
		if currRowCount >= int64(expectedRowCount) {
			return nil
		}
		log.Infof("Waiting for '%s' table '%s' large then %d records.. (%d sec), current total: %d",
			loc, tableName, expectedRowCount, retry, currRowCount)
		time.Sleep(1 * time.Second)
	}

	return fmt.Errorf("Expected large then %d records, but got %d", expectedRowCount, currRowCount)
}

func rowCountLessThenTimeoutSeconds(loc, tableName string, expectedRowCount, timeoutSec int) error {
	db, err := getDBInstance(loc)
	if err != nil {
		return err
	}

	var currRowCount int64
	var retry int
	for retry = 0; retry < timeoutSec; retry++ {
		db.Table(tableName).Count(&currRowCount)
		if currRowCount <= int64(expectedRowCount) {
			return nil
		}
		log.Infof("Waiting for '%s' table '%s' less then %d records.. (%d sec), current total: %d",
			loc, tableName, expectedRowCount, retry, currRowCount)
		time.Sleep(1 * time.Second)
	}

	return fmt.Errorf("Expected less then %d records, but got %d", expectedRowCount, currRowCount)
}

func insertDummyDataFromID(loc, tableName string, total int, beginID int) error {
	db, err := getDBInstance(loc)
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

func cleanUpTable(loc, tableName string) error {
	db, err := getDBInstance(loc)
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

func updateRowDummyDataFromID(loc, tableName string, total, beginID int) error {
	db, err := getDBInstance(loc)
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

	/* 假設都會 update 成功, 不做檢查；而且後面的 step 會比對 source & target 的資料時, 會有檢查
	// Verify each record is updated
	for i := beginID; i < total+beginID; i++ {
		var account Account
		err = db.Table("Accounts").Where("ID = ?", i).First(&account).Error
		if err != nil {
			return fmt.Errorf("Failed to retrieve '%d th' record: %v", i, err)
		}
		if !strings.Contains(account.Name, "updated") {
			return fmt.Errorf("Failed to update '%d th' record", i)
		}
		log.Debugf("'%s' table updated - ID '%d', Name: '%s', Phone: '%s'",
			loc, account.ID, account.Name, account.Phone)
	}
	*/

	return nil
}

func getCount(db *gorm.DB, tableName string) (int64, error) {
	var count int64
	err := db.Table(tableName).Count(&count).Error
	return count, err
}

func compareRecords(sourceDB, targetDB *gorm.DB) (int, error) {
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

func verifyFromToRowCountAndContentTimeoutSeconds(locTo, tableName, locFrom string, timeoutSec int) error {
	// compare source/target table row count
	sourceDB, err := getDBInstance(locFrom)
	if err != nil {
		return err
	}
	targetDB, err := getDBInstance(locTo)
	if err != nil {
		return err
	}
	srcRowCount, err := getCount(sourceDB, tableName)
	if err != nil {
		return err
	}

	// Check number of records
	var retry int
	var targetRowCount int64
	for retry = 0; retry < timeoutSec; retry++ {
		targetRowCount, err := getCount(targetDB, tableName)
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
		lastMatchID, err := compareRecords(sourceDB, targetDB)
		if err == nil {
			return nil
		}
		log.Infof("Waiting for '%s' table '%s' to has %d same content.. (%d sec), last match ID %d",
			locTo, tableName, srcRowCount, retry, lastMatchID)
		time.Sleep(1 * time.Second)
	}
	return fmt.Errorf("Content of table '%s' is not the same after %d second", tableName, timeoutSec)
}

/*
func verifyFromToRowCountTimeoutSeconds(locTo, tableName, locFrom string, timeoutSec int) error {
	// compare source/target table row count
	sourceDB, err := getDBInstance(locFrom)
	if err != nil {
		return err
	}
	targetDB, err := getDBInstance(locTo)
	if err != nil {
		return err
	}
	srcRowCount, err := getCount(sourceDB, tableName)
	if err != nil {
		return err
	}

	// Check number of records
	var retry int
	var targetRowCount int64
	for retry = 0; retry < timeoutSec; retry++ {
		targetRowCount, err = getCount(targetDB, tableName)
		if err != nil {
			return err
		}
		log.Infof("Waiting for '%s' table '%s' to has %d records.. (%d sec), current total: %d",
			locTo, tableName, srcRowCount, retry, targetRowCount)
		if targetRowCount == srcRowCount {
			return nil
		}
		time.Sleep(1 * time.Second)
	}

	return fmt.Errorf("Number of records in table '%s' is %d, expected %d after %d second",
		tableName, targetRowCount, srcRowCount, timeoutSec)
}

func getUpdatedCount(db *gorm.DB, tableName string, startID int) (int64, error) {
	var count int64
	err := db.Table(tableName).Where("id >= ? AND name LIKE '%updated%'", startID).Count(&count).Error
	return count, err
}

func verifyFromToUpdatedTimeoutSeconds(locTo, tableName, locFrom string, startID, total, timeoutSec int) error {
	// compare source/target table row count
	sourceDB, err := getDBInstance(locFrom)
	if err != nil {
		return err
	}
	targetDB, err := getDBInstance(locTo)
	if err != nil {
		return err
	}
	srcRowCount, err := getCount(sourceDB, tableName)
	if err != nil {
		return err
	}

	// Check number of records
	var retry int
	var targetRowCount int64
	for retry = 0; retry < timeoutSec; retry++ {
		targetRowCount, err = getCount(targetDB, tableName)
		if err != nil {
			return err
		}

		if targetRowCount == srcRowCount {
			break
		}
		log.Infof("Waiting for '%s' table '%s' to has %d total records.. (%d sec), current total: %d",
			locTo, tableName, srcRowCount, retry, targetRowCount)
		time.Sleep(1 * time.Second)
	}

	// Check number of updated records in the target table
	var updatedCount int64
	for retry = 0; retry < timeoutSec; retry++ {
		updatedCount, err := getUpdatedCount(targetDB, tableName, startID)
		if err != nil {
			return err
		}
		log.Infof("Waiting for '%s' table '%s' to has %d updated records.. (%d sec), current updated total: %d",
			locTo, tableName, total, retry, updatedCount)
		if updatedCount == int64(total) {
			return nil
		}
		time.Sleep(1 * time.Second)
	}
	return fmt.Errorf("Number of updated records in table '%s' is %d, expected %d after %d second",
		tableName, updatedCount, total, timeoutSec)
}
*/

func serviceReady(srvName, filePath string, timeoutSec int) error {
	for i := 0; i < timeoutSec; i++ {
		if _, err := os.Stat(filePath); err == nil {
			log.Infof("'%s' service flag file '%s' be found after %d seconds\n", srvName, filePath, i)
			return nil
		}
		time.Sleep(1 * time.Second)
	}
	return fmt.Errorf("'%s' service '%s' not ready after %d second", srvName, filePath, timeoutSec)
}

func InitializeScenario(ctx *godog.ScenarioContext) {
	ctx.Given(`^啟動 "([^"]*)" 服務$`, dockerComposeServiceStart)
	ctx.Given(`^初始化 "([^"]*)" 資料表 Accounts$`, dbServerInit)
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
