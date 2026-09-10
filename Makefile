.PHONY: help setup scan serve stats test test-go test-py lint up down

CONFIG ?= shanyrak.toml
PY     ?= scraper/.venv/bin/python

help:
	@grep -E '^[a-z-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk -F':.*## ' '{printf "%-10s %s\n", $$1, $$2}'

setup: ## create the scraper virtualenv
	python3 -m venv scraper/.venv
	$(PY) -m pip install --quiet --upgrade pip
	$(PY) -m pip install --quiet -e "scraper[dev]"

scan: ## fetch listings into the databases
	$(PY) -m shanyrak_scraper --config $(CONFIG) scan

stats: ## show what the databases hold
	$(PY) -m shanyrak_scraper --config $(CONFIG) stats

serve: ## run the map server on http://localhost:8080
	go run ./cmd/shanyrak -config $(CONFIG)

test: test-go test-py ## run every test

test-go:
	go test ./...

test-py:
	cd scraper && .venv/bin/python -m pytest -q

lint: ## vet and format check
	go vet ./...
	@test -z "$$(gofmt -l .)" || { echo "gofmt needed:"; gofmt -l .; exit 1; }

up: ## run server and scraper in docker
	docker compose up --build -d

down:
	docker compose down
