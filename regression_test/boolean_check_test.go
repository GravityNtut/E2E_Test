package boolcheck

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/BrobridgeOrg/gravity-sdk/v2/core"
	subscriber_sdk "github.com/BrobridgeOrg/gravity-sdk/v2/subscriber"
	gravity_sdk_types_product_event "github.com/BrobridgeOrg/gravity-sdk/v2/types/product_event"
	log "github.com/sirupsen/logrus"
	"google.golang.org/protobuf/proto"

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
			StopOnFailure: false,
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
	Nats     serverInfo `json:"nats"`
}

type serverInfo struct {
	Type     string `json:"type"`
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Username string `json:"username"`
	Password string `json:"password"`
	Database string `json:"database"`
}

type JSONData struct {
	Event   string `json:"event"`
	Payload string `json:"payload"`
}

type Products struct {
	ID       int
	Name     string
	Price    float64
	Stock    int
	Obsolete bool
	ModTime  time.Time
}

var product Products

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

func ConnectToDB(s *serverInfo) (*gorm.DB, error) {
	if s.Type == "mysql" {
		dsn := fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?charset=utf8mb4&parseTime=True&loc=Local",
			s.Username, s.Password, s.Host, s.Port, s.Database)
		db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{})
		if err != nil {
			return nil, fmt.Errorf("failed to connect to database: %v", err)
		}
		return db, nil
	} else if s.Type == "mssql" {
		dsn := fmt.Sprintf("sqlserver://%s:%s@%s:%d?database=%s",
			s.Username, s.Password, s.Host, s.Port, s.Database)
		db, err := gorm.Open(sqlserver.Open(dsn), &gorm.Config{})
		if err != nil {
			return nil, fmt.Errorf("failed to connect to database: %v", err)
		}
		return db, nil
	}
	return nil, fmt.Errorf("invalid database type '%s'", s.Type)
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
		return fmt.Errorf("failed to read create_test_db.sql: %v", err)
	}
	db.Exec(string(str))
	return nil
}

func InitProductsTable(s *serverInfo, createTableFilePath string) error {
	var err error
	sourceDB, err := ConnectToDB(s)
	if err != nil {
		return fmt.Errorf("failed to connect to '%s' database: %v", s.Type, err)
	}

	db, err := sourceDB.DB()
	if err != nil {
		return fmt.Errorf("failed to connect to '%s' database: %v", s.Type, err)
	}
	str, err := os.ReadFile(createTableFilePath)
	if err != nil {
		return fmt.Errorf("failed to read create_table.sql: %v", err)
	}
	if _, err := db.Exec(string(str)); err != nil {
		return fmt.Errorf("failed to create table: %v", err)
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
		createTestDBFilePath = "./assets/mssql/create_test_db.sql"
		serverInfo = &config.SourceDB
		createTableFilePath = "./assets/mssql/create_table.sql"
	case TargetMySQL:
		info := &config.TargetDB
		dsn := fmt.Sprintf("%s:%s@tcp(%s:%d)/?charset=utf8mb4&parseTime=True&loc=Local",
			info.Username, info.Password, info.Host, info.Port)

		dialector = mysql.Open(dsn)
		createTestDBFilePath = "./assets/mysql/create_test_db.sql"

		serverInfo = &config.TargetDB
		createTableFilePath = "./assets/mysql/create_table.sql"
	default:
		return fmt.Errorf("invalid database type '%s'", dbStr)
	}

	if err := CreateTestDB(dialector, createTestDBFilePath); err != nil {
		return err
	}
	if err := InitProductsTable(serverInfo, createTableFilePath); err != nil {
		return err
	}
	return nil
}

func ContainerAndProcessReadyTimeoutSeconds(ctName string, timeoutSec int) error {
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
		return fmt.Errorf("invalid container name '%s'", ctName)
	}
}

func DockerComposeServiceStart(serviceName string, timeout int) error {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return fmt.Errorf("failed to create docker client: %v", err)
	}
	if err := cli.ContainerStart(context.Background(), serviceName, container.StartOptions{}); err != nil {
		return fmt.Errorf("failed to start container '%s': %v", serviceName, err)
	}
	return ContainerAndProcessReadyTimeoutSeconds(serviceName, timeout)
}

func DatabaseLifeCheck(dialector gorm.Dialector, timeout int) (*gorm.DB, error) {
	for i := 0; i < timeout; i += 5 {
		db, err := gorm.Open(dialector, &gorm.Config{})
		if err != nil {
			log.Infof("Attempting to connect to %s Database (%d sec)", dialector.Name(), i)
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
			log.Infof("Waiting for %s Database to become available (%d sec)", dialector.Name(), i)
			time.Sleep(5 * time.Second)
			continue
		}
		return db, nil
	}
	return nil, fmt.Errorf("timeout connecting to the %s Database", dialector.Name())
}

func NatsLifeCheck(timeout int) (*nats.Conn, error) {
	for i := 0; i < timeout; i++ {
		nc, err := nats.Connect(fmt.Sprintf("nats://%s:%d", config.Nats.Host, config.Nats.Port))
		if err != nil {
			log.Infoln("Unable to connect to the NATS server. Retry after 1 second")
			time.Sleep(1 * time.Second)
			continue
		}
		return nc, nil
	}
	return nil, fmt.Errorf("timeout connecting to NATS server")
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
				cmdLine := processInfo[7]
				if cmdLine == "/"+processName || cmdLine == processName {
					return nil
				}

				if len(cmdLine) > 0 {
					cmds := strings.Split(cmdLine, " ")
					if len(cmds) > 0 {
						for _, cmd := range cmds {
							if cmd == processName || cmd == "/"+processName {
								return nil
							}
						}
					}
				}
			}
		}
	}

	return fmt.Errorf("process %s is not running in container %s", processName, containerName)
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
	return fmt.Errorf("the service '%s' timed out within %d seconds", ctName, timeout)

}

func CreateDataProduct() error {
	nc, err := nats.Connect(fmt.Sprintf("nats://%s:%d", config.Nats.Host, config.Nats.Port))
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
		return nil, fmt.Errorf("invalid database location '%s'", loc)
	}

}

func InsertARecord(loc, tableName string) error {
	db, err := GetDBInstance(loc)
	if err != nil {
		return err
	}
	log.Infof("Inserting a record to '%s' - '%s'",
		loc, tableName)

	query := "INSERT INTO Products (Name, Price, Stock, ModTime) VALUES ('pd1', 100, 100, GETDATE());"
	result := db.Exec(query)
	if result.Error != nil {
		log.Printf("Failed to insert record: %v", result.Error)
	}

	return nil
}

func ExecuteContainerCommand(containerID string, cmd []string) (string, error) {
	ctx := context.Background()

	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return "", fmt.Errorf("error creating Docker client: %v", err)
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
		return "", fmt.Errorf("error attaching to exec instance: %v", err)
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
		return "", fmt.Errorf("failed to get token from result: %s", result)
	}
	return parts[1], nil
}

func InitAtomicService() error {
	token, err := GetToken()
	if err != nil {
		return err
	}
	inputFileName := "assets/unprocessed_cred.json"
	byteValue, err := os.ReadFile(inputFileName)
	if err != nil {
		return fmt.Errorf("failed to read JSON file: %v", err)
	}

	var data map[string]map[string]string
	if err := json.Unmarshal(byteValue, &data); err != nil {
		return fmt.Errorf("failed to parse JSON file: %v", err)
	}

	for _, component := range data {
		if _, exist := component["accessToken"]; exist {
			component["accessToken"] = token
		}
	}

	outputFileName := "tmp/unencrypted_cred.json"
	modifiedFile, err := os.Create(outputFileName)
	if err != nil {
		return fmt.Errorf("failed to create output JSON file: %v", err)
	}

	defer func() {
		if err := modifiedFile.Close(); err != nil {
			log.Fatal(err)
		}
	}()

	modifiedJSON, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal modified JSON: %v", err)
	}

	if _, err := modifiedFile.Write(modifiedJSON); err != nil {
		return fmt.Errorf("failed to write to %s JSON file: %v", outputFileName, err)
	}

	cmd := exec.Command("sh", "./assets/flowEnc.sh", outputFileName,
		"./assets/atomic", ">", "./assets/atomic/flows_cred.json")
	credFile, err := os.Create("./assets/atomic/flows_cred.json")
	if err != nil {
		return fmt.Errorf("failed to create flows_cred.json: %v", err)
	}
	var stderr bytes.Buffer
	cmd.Stdout = credFile
	cmd.Stderr = &stderr
	err = cmd.Run()
	if err != nil {
		return fmt.Errorf("failed to execute flowEnc.sh: %s", stderr.String())
	}
	return nil
}

func Base64ToString(base64Str string) (string, error) {
	decodedBytes, err := base64.StdEncoding.DecodeString(base64Str)
	if err != nil {
		return "", err
	}
	return string(decodedBytes), nil
}

func CheckDBData(loc string) error {
	db, err := GetDBInstance(loc)
	if err != nil {
		return err
	}

	timeout := 10
	timeoutState := true
	for i := 0; i < timeout; i++ {
		time.Sleep(1 * time.Second)
		var count int64
		db.Table("Products").Count(&count)
		if count != 0 {
			timeoutState = false
			break
		}
	}

	if timeoutState {
		return fmt.Errorf("get %s data timeout", loc)
	}

	err = db.Table("Products").First(&product).Error
	if err != nil {
		return fmt.Errorf("failed to query Products table: %v", err)
	}

	if product.Obsolete {
		return fmt.Errorf("data in %s Obsolete is true", loc)
	}

	return nil
}

func CheckNatsStreamResult() error {
	nc, _ := nats.Connect(fmt.Sprintf("nats://%s:%d", config.Nats.Host, config.Nats.Port))
	defer nc.Close()

	js, err := nc.JetStream()
	if err != nil {
		log.Fatal(err)
	}

	ch := make(chan *nats.Msg, 1)
	if _, err := js.ChanSubscribe("$GVT.default.EVENT.*", ch); err != nil {
		return fmt.Errorf("jetstream subscribe failed: %v", err)
	}

	var m *nats.Msg
	select {
	case m = <-ch:

	case <-time.After(10 * time.Second):
		return errors.New("subscribe out of time")
	}

	var jsonData JSONData
	if err := json.Unmarshal(m.Data, &jsonData); err != nil {
		return fmt.Errorf("json unmarshal failed: %v", err)
	}

	payload, err := Base64ToString(jsonData.Payload)
	if err != nil {
		return fmt.Errorf("base64 decode failed: %v", err)
	}

	var result map[string]interface{}
	if err := json.Unmarshal([]byte(payload), &result); err != nil {
		return fmt.Errorf("json unmarshal failed: %v", err)
	}

	obsolete, ok := result["Obsolete"].(bool)
	if !ok {
		return fmt.Errorf("Obsolete field not found or is not a boolean")
	}

	if obsolete {
		return fmt.Errorf("Obsolete is true")
	}

	return nil
}

func GetSubscribeResult(client *core.Client) (map[string]interface{}, error) {
	done := make(chan bool)
	acOpts := subscriber_sdk.NewOptions()
	acOpts.Domain = "default"
	acOpts.Verbose = true
	s := subscriber_sdk.NewSubscriberWithClient("", client, acOpts)
	var result map[string]interface{}
	sub, err := s.Subscribe("products", func(msg *nats.Msg) {
		var pe gravity_sdk_types_product_event.ProductEvent
		err := proto.Unmarshal(msg.Data, &pe)
		if err != nil {
			fmt.Printf("failed to parsing product event: %v", err)
			if err := msg.Ack(); err != nil {
				fmt.Printf("failed to acknowledge message: %v", err)
			}
			return
		}

		r, err := pe.GetContent()
		if err != nil {
			fmt.Printf("failed to parsing content: %v", err)
			if err := msg.Ack(); err != nil {
				fmt.Printf("failed to acknowledge message: %v", err)
			}

			return
		}
		result = r.AsMap()

		if err := msg.Ack(); err != nil {
			fmt.Printf("failed to acknowledge message: %v", err)
		}
		done <- true
	}, subscriber_sdk.Partition(-1), subscriber_sdk.StartSequence(1))
	if err != nil {
		return nil, fmt.Errorf("failed to subscribe: %v", err)
	}

	select {
	case <-done:
		if err := sub.Close(); err != nil {
			return nil, fmt.Errorf("failed to close subscription: %v", err)
		}
	case <-time.After(5 * time.Second):
		if err := sub.Close(); err != nil {
			return nil, fmt.Errorf("failed to close subscription: %v", err)
		}
		return nil, fmt.Errorf("subscribe timeout")
	}

	return result, nil
}

func CheckSubResult() error {
	client := core.NewClient()
	options := core.NewOptions()
	if err := client.Connect(fmt.Sprintf("%s:%d", config.Nats.Host, config.Nats.Port), options); err != nil {
		return fmt.Errorf("failed to connect to service host port: %v", err)
	}
	result, err := GetSubscribeResult(client)

	if err != nil {
		return fmt.Errorf("Failed to get result from subscribe sdk: %v", err)
	}

	if result["Obsolete"] != 0 {
		return fmt.Errorf("Obsolete get by sub sdk is true")
	}

	return nil
}

func InitializeScenario(ctx *godog.ScenarioContext) {
	ctx.After(func(ctx context.Context, _ *godog.Scenario, _ error) (context.Context, error) {
		if err := CloseAllServices(); err != nil {
			log.Errorf("failed to close all services: %v", err)
		}
		return ctx, nil
	})

	ctx.Given(`^Create all services$`, CreateServices)
	ctx.Given(`^Load the initial configuration file$`, LoadConfig)
	ctx.Given(`^Start the "([^"]*)" service \(timeout "(\d+)"\)$`, DockerComposeServiceStart)
	ctx.Given(`^Initialize the "([^"]*)" table Products$`, DBServerInit)
	ctx.Given(`^Create data product Products$`, CreateDataProduct)
	ctx.Given(`^Set up atomic flow document$`, InitAtomicService)

	ctx.Given(`^"([^"]*)" table "([^"]*)" inserted a record which has false boolean value$`, InsertARecord)
	ctx.Then(`^Check the "([^"]*)" table Products has a record with false value$`, CheckDBData)
	ctx.Then(`^Check the nats stream default domain has a record with false value$`, CheckNatsStreamResult)
	ctx.Then(`^Check the subscribe sdk result has a record with false value$`, CheckSubResult)
	ctx.Then(`^Check the "([^"]*)" table Products has a record with false value$`, CheckDBData)
}
