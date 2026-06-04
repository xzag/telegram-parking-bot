APP_NAME := parking-bot

GOOS ?= linux
GOARCH ?= amd64

.PHONY: build
build:
	CGO_ENABLED=0 GOOS=$(GOOS) GOARCH=$(GOARCH) go build -o $(APP_NAME) ./cmd/bot

.PHONY: run
run:
	go run ./cmd/bot

.PHONY: tidy
tidy:
	go mod tidy

.PHONY: test
test:
	go test ./...

.PHONY: clean
clean:
	rm -f $(APP_NAME)

.PHONY: deploy
deploy: build
	scp $(APP_NAME) root@$(SERVER):/tmp/$(APP_NAME)

.PHONY: install
install:
	sudo systemctl stop parking-bot
	sudo cp $(APP_NAME) /opt/parking-bot/$(APP_NAME)
	sudo chown parking:parking /opt/parking-bot/$(APP_NAME)
	sudo chmod +x /opt/parking-bot/$(APP_NAME)
	sudo systemctl start parking-bot

.PHONY: restart
restart:
	sudo systemctl restart parking-bot

.PHONY: logs
logs:
	journalctl -u parking-bot -f

.PHONY: status
status:
	systemctl status parking-bot
