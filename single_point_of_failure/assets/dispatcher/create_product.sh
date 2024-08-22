#!/bin/sh

# create data prodcut
/gravity-cli product create accounts --desc="e2etest" --enabled \
    --schema=/assets/dispatcher/schema_test.json \
    -s "${GRAVITY_DISPATCHER_GRAVITY_HOST}:${GRAVITY_DISPATCHER_GRAVITY_PORT}"
if [ $? -ne 0 ]; then
    echo "@@ Failed to create product !!!"
    exit 1
else
    echo "## Product has been created."
fi
# create product ruleset
## accountCreated
/gravity-cli product ruleset add accounts accountCreated --enabled \
    --event=accountCreated --method=create \
    --handler=/assets/dispatcher/handler_test.js \
    --schema=/assets/dispatcher/schema_test.json \
    --pk="id" \
    -s "${GRAVITY_DISPATCHER_GRAVITY_HOST}:${GRAVITY_DISPATCHER_GRAVITY_PORT}"
if [ $? -ne 0 ]; then
    echo "@@ Failed to create product ruleset 'accountCreated' !!!"
    exit 1
else
    echo "## Product ruleset 'accountCreated' has been created."
fi

## accountUpdated
/gravity-cli product ruleset add accounts accountUpdated --enabled \
    --event=accountUpdated --method=update \
    --handler=/assets/dispatcher/handler_test.js \
    --schema=/assets/dispatcher/schema_test.json \
    --pk="id" \
    -s "${GRAVITY_DISPATCHER_GRAVITY_HOST}:${GRAVITY_DISPATCHER_GRAVITY_PORT}"
if [ $? -ne 0 ]; then
    echo "@@ Failed to create product ruleset 'accountUpdated' !!!"
    exit 1
else
    echo "## Product ruleset 'accountUpdated' has been created."
fi

## accountDeleted
/gravity-cli product ruleset add accounts accountDeleted --enabled \
    --event=accountDeleted --method=delete \
    --handler=/assets/dispatcher/handler_test.js \
    --schema=/assets/dispatcher/schema_test.json \
    --pk="id" \
    -s "${GRAVITY_DISPATCHER_GRAVITY_HOST}:${GRAVITY_DISPATCHER_GRAVITY_PORT}"
if [ $? -ne 0 ]; then
    echo "@@ Failed to create product ruleset 'accountDeleted' !!!"
    exit 1
else
    echo "## Product ruleset 'accountDeleted' has been created."
fi

exit 0