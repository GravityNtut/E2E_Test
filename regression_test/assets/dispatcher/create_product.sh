#!/bin/sh

# create data prodcut
/gravity-cli product create products --desc="bool check" --enabled \
    --schema=/assets/dispatcher/schema_test.json \
    -s "${GRAVITY_DISPATCHER_GRAVITY_HOST}:${GRAVITY_DISPATCHER_GRAVITY_PORT}"
if [ $? -ne 0 ]; then
    echo "@@ Failed to create product !!!"
    exit 1
else
    echo "## Product has been created."
fi
# create product ruleset
## productCreated
/gravity-cli product ruleset add products productCreated --enabled \
    --event=productCreated --method=create \
    --handler=/assets/dispatcher/handler_test.js \
    --schema=/assets/dispatcher/schema_test.json \
    --pk="id" \
    -s "${GRAVITY_DISPATCHER_GRAVITY_HOST}:${GRAVITY_DISPATCHER_GRAVITY_PORT}"
if [ $? -ne 0 ]; then
    echo "@@ Failed to create product ruleset 'productCreated' !!!"
    exit 1
else
    echo "## Product ruleset 'productCreated' has been created."
fi

exit 0