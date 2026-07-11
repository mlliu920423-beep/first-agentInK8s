# Simple developer commands. Assumes bash (git-bash on Windows works).
# Windows PowerShell users can also just run each recipe manually.

.PHONY: web-build assets build run dev-web docker deploy port-forward clean

# --- local build/run ---

web-build:
	cd web && npm install && npm run build

assets: web-build
	rm -rf internal/webassets/dist && mkdir -p internal/webassets/dist
	cp -r web/dist/. internal/webassets/dist/
	touch internal/webassets/dist/.gitkeep

build: assets
	go build -o server ./cmd/server

run: build
	./server

# UI dev server + Go backend must be run in two shells:
#   Shell A: make dev-web
#   Shell B: ARK_API_KEY=... ARK_MODEL_ID=... go run ./cmd/server
dev-web:
	cd web && npm run dev

# --- container / k8s (Docker Desktop built-in) ---
# Docker Desktop 内置 k8s 与本机 docker 共用同一个镜像 daemon，
# 所以 build 完镜像立刻就能被集群看到，不需要 kind load 那一步。

IMAGE ?= eino-demo:local

docker:
	docker build -t $(IMAGE) .

deploy: docker
	kubectl apply -f k8s/deployment.yaml -f k8s/service.yaml
	kubectl rollout restart deployment/eino-demo
	kubectl rollout status deployment/eino-demo

port-forward:
	kubectl port-forward svc/eino-demo 8080:80

clean:
	rm -rf server web/dist web/node_modules internal/webassets/dist/*
	touch internal/webassets/dist/.gitkeep
