# pglockr dev test bench.
#
# Quick start:
#   make up            # start PostgreSQL
#   make pglockr       # run pglockr against it (in another terminal)
#   make load-chain    # manufacture a blocking tree
#   make tree          # see it from the DB; or open the UI
#   make load-reset    # clear the load
#   make down          # stop and wipe
#
# pglockr connects with this DSN (pg_monitor + pg_signal_backend):
DSN  := postgres://pglockr_ro:pglockr@localhost:55432/app?sslmode=disable
TOKEN := dev

COMPOSE := docker compose
SC := bash scenarios

.PHONY: help up down restart logs ps psql wait dsn tree pglockr \
        load-simple load-chain load-fanout load-forest load-deadlock load-reset clean

help: ## show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) \
		| awk 'BEGIN{FS=":.*?## "}{printf "  \033[36m%-16s\033[0m %s\n",$$1,$$2}'

## --- stack lifecycle ---
up: ## start the database and wait until it is ready
	$(COMPOSE) up -d
	@$(MAKE) --no-print-directory wait
	@echo "DB ready. DSN: $(DSN)"

wait: ## block until the database healthcheck passes
	@echo "waiting for postgres..."
	@for i in $$(seq 1 30); do \
		if [ "$$($(COMPOSE) ps -q db | xargs docker inspect -f '{{.State.Health.Status}}' 2>/dev/null)" = "healthy" ]; then \
			echo "healthy"; exit 0; fi; sleep 1; done; \
		echo "timed out waiting for db" >&2; exit 1

down: ## stop the database and delete its data volume
	$(COMPOSE) down -v

restart: down up ## recreate the stack from scratch

logs: ## tail database logs (lock waits, deadlocks)
	$(COMPOSE) logs -f db

ps: ## show container status
	$(COMPOSE) ps

psql: ## open a superuser psql shell
	$(COMPOSE) exec db psql -U postgres -d app

dsn: ## print the DSN to use with pglockr
	@echo "$(DSN)"

## --- run pglockr against this bench ---
pglockr: ## build & run pglockr pointed at this database (token: dev)
	cd .. && PGLOCKR_DSN="$(DSN)" PGLOCKR_TOKEN="$(TOKEN)" PGLOCKR_DB_PATH=history.db go run ./cmd/pglockr

## --- load generation ---
load-simple: ## one idle-in-txn holder + one waiter
	@$(SC)/simple.sh
load-chain: ## three-deep chain (leaf -> mid -> root)
	@$(SC)/chain.sh
load-fanout: ## one root with four waiters
	@$(SC)/fanout.sh
load-forest: ## two independent blocking trees
	@$(SC)/forest.sh
load-deadlock: ## transient deadlock (PG aborts a victim)
	@$(SC)/deadlock.sh
load-reset: ## terminate all load sessions, clear locks
	@$(SC)/reset.sh

tree: ## print the current blocking tree from the DB
	@$(SC)/tree.sh

clean: load-reset ## clear load (DB keeps running)
	@echo "cleared. 'make down' to stop the database."
