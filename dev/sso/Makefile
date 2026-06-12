# SSO demo: oauth2-proxy + a test OIDC provider in front of pglockr.
COMPOSE := docker compose
DSN := postgres://pglockr_ro:pglockr@localhost:55432/app?sslmode=disable

.PHONY: help up down logs pglockr

help: ## show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) \
		| awk 'BEGIN{FS=":.*?## "}{printf "  \033[36m%-10s\033[0m %s\n",$$1,$$2}'

up: ## start oauth2-proxy + the test IdP
	$(COMPOSE) up -d
	@echo "oauth2-proxy on http://localhost:4180  (IdP on http://localhost:8083)"
	@echo "Now run 'make pglockr' here, then open http://localhost:4180"

down: ## stop the SSO stack
	$(COMPOSE) down

logs: ## tail oauth2-proxy + IdP logs
	$(COMPOSE) logs -f

## pglockr: run pglockr on the host in proxy/network mode behind the proxy.
## Needs the dev DB up (cd ../ && make up).
pglockr:
	cd ../.. && PGLOCKR_DSN="$(DSN)" PGLOCKR_HTTP_ADDR=":8111" \
		go run ./cmd/pglockr -config dev/sso/pglockr-proxy.yaml
