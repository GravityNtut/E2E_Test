# E2E_Test
This is end to end test repository for Gravity.

## Require services
- [Gravity-Dispatcher](https://github.com/BrobridgeOrg/gravity-dispatcher)
- [Gravity-Atomic](https://github.com/BrobridgeOrg/atomic-gravity)
- [Nats jetstream](https://docs.nats.io/nats-concepts/jetstream)
- [Gravity-Adapter-mssql](https://github.com/BrobridgeOrg/gravity-adapter-mssql)
- sql-server 
- mysql

## File Structure
The repository is organized by test tasks, where each folder represents a specific test task:
- `.feature` files: Contain test scenarios and detailed test steps
- `config.json`: Environment settings for the test
- `assets/`: Directory containing required test files
- `docker-compose.yaml`: Service definitions for the test environment

For example, in `single_point_of_failure`
This test task includes:
- Two feature files:
    - `service_restart_during_data_transfer.feature`
    - `single_point_failure.feature`
- Supporting files and directories for test execution

Directory structure shown below:

```
.
├── regression_test
│   ├── assets
│   ├── tmp
│   ├── boolean_check.feature
│   ├── boolean_check_test.go
│   ├── config.json
│   └── docker-compose.yaml
├── single_point_of_failure
│   ├── assets
│   ├── tmp
│   ├── config.json
│   ├── docker-compose.yaml
│   ├── service_restart_during_data_transfer.feature
│   ├── single_point_failure.feature
│   ├── single_point_failure_test.go
│   └── UsefulCmd.txt
```

## Usage

Clone this repository:
```
git clone git@github.com:GravityNtut/E2E_Test.git
cd E2E_Test
```

### Execute through docker compose
1. Change to the specific test task directory:
    ```sh
    cd <test_task_directory>
    ```

2. Run the test (takes approximately 30 minutes):
    ```sh
    go test --timeout=60m
    ```

### Execute through kubernetes
- TODO
