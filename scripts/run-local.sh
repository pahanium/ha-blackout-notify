#!/bin/bash
# Script to run the bot locally with .env file

set -a  # automatically export all variables
source .env
set +a

# Override database path for local development (avoid /data which might not exist)
export STATS_DB_PATH="./power_stats.db"

# Run the bot
./bin/blackout-notify
