# ---- 1) build react bundle ----
FROM node:20-alpine AS web
WORKDIR /web
COPY web/package.json web/package-lock.json* ./
RUN npm install --no-audit --no-fund
COPY web/ ./
RUN npm run build

# ---- 2) build go binary ----
FROM golang:1.26 AS gobuild
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
# Drop the placeholder .gitkeep-only dist and inject the real build output
# under the exact path //go:embed points at.
RUN rm -rf internal/webassets/dist && mkdir -p internal/webassets/dist
COPY --from=web /web/dist/ internal/webassets/dist/
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /out/server ./cmd/server

# ---- 3) minimal runtime ----
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=gobuild /out/server /server
# Embed the agent yaml configs at a known location; overridable via AGENTS_DIR.
COPY agents/ /agents/
ENV AGENTS_DIR=/agents
# MCP server declarations; overridable via MCP_DIR. See docs/adr/005 for schema.
COPY mcp/ /mcp/
ENV MCP_DIR=/mcp
EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["/server"]
