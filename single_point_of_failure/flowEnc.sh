#!/bin/bash
#
# Encrypt a JSON file to the format used in Node-RED's flows_cred.json
#
# Usage
# ./node-red-encrypt-flows-cred.sh ./input.json ./node_red_data

# 讀取輸入 JSON 檔案和 Node-RED 資料目錄
INPUT_JSON_FILE=$1
NODE_RED_DATA_DIR=$2

# 從 Node-RED 配置中取得金鑰和 IV
CREDENTIAL_SECRET=$(jq -j '._credentialSecret' "$NODE_RED_DATA_DIR/.config.runtime.json" | sha256sum | cut -c 1-64)
IV=$(openssl rand -hex 16)

# 加密輸入文件
ENCRYPTED_CONTENT=$(openssl enc -aes-256-ctr -e -base64 -A -iv "$IV" -K "$CREDENTIAL_SECRET" -in "$INPUT_JSON_FILE")

# flows_cred.json 格式的輸出
echo "{\"$\":\"$IV$ENCRYPTED_CONTENT\"}"