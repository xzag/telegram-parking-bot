#!/usr/bin/env bash
set -euo pipefail

APP_NAME="parking-bot"
APP_USER="parking"
APP_DIR="/opt/parking-bot"
SERVICE_NAME="parking-bot"

cd "$(dirname "$0")/.."

if [[ ! -f "./${APP_NAME}" ]]; then
  echo "Binary ./${APP_NAME} not found"
  exit 1
fi

echo "Stopping service..."
sudo systemctl stop "${SERVICE_NAME}" || true

echo "Installing binary..."
sudo install -o "${APP_USER}" -g "${APP_USER}" -m 0755 "./${APP_NAME}" "${APP_DIR}/${APP_NAME}"

echo "Starting service..."
sudo systemctl start "${SERVICE_NAME}"

echo "Status:"
sudo systemctl --no-pager status "${SERVICE_NAME}"
