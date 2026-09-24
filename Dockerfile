# ---- backend build stage --------------------------------------------------
# Build context is the repository root.
FROM golang:1.22 AS build
WORKDIR /src

COPY backend/go.mod backend/go.sum ./
RUN GOPROXY=https://proxy.golang.org,https://goproxy.cn,direct go mod download

COPY backend/ ./

# The automated tests run inside the image build, locking the core
# guarantees: topological recalc (no stale reads), direct/indirect cycle
# detection, reference shifting on row/column insert-delete, last-writer-wins
# conflict notices and cross-user undo dependency rollback.
RUN go test ./...

RUN CGO_ENABLED=0 GOOS=linux go build -o /out/server ./cmd/server

# ---- frontend build stage -------------------------------------------------
FROM node:20-alpine AS frontend
WORKDIR /web
COPY frontend/package.json frontend/package-lock.json* ./
RUN npm install --no-audit --no-fund
COPY frontend/ ./
RUN npm run build
# Vite emits the bundle at /web/../web/dist => /web-dist

# ---- runtime stage --------------------------------------------------------
FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /app
COPY --from=build /out/server /app/server
COPY --from=frontend /web-dist /app/web/dist
ENV STATIC_DIR=/app/web/dist \
    ADDR=:8080
EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["/app/server"]
