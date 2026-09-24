# syntax=docker/dockerfile:1

# ---- build stage: dependencies, tests and binary are all produced here ----
FROM golang:1.22-bookworm AS build
WORKDIR /src

# Cache module downloads first.
COPY backend/go.mod backend/go.sum ./
RUN go mod download

COPY backend/ ./

# Tests run as part of the image build: topo recalculation, cycle detection,
# row/column shift adjustment, last-writer-wins and cross-user undo rollback.
RUN CGO_ENABLED=0 go test ./...

RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o /out/server ./cmd/server

# ---- frontend stage ----
FROM node:20-bookworm AS frontend
WORKDIR /web
COPY frontend/package.json frontend/package-lock.json* ./
RUN npm install
COPY frontend/ ./
RUN npm run build

# ---- runtime stage ----
FROM debian:bookworm-slim AS runtime
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates && \
    rm -rf /var/lib/apt/lists/*
WORKDIR /app
COPY --from=build /out/server /app/server
COPY --from=frontend /web/dist /app/frontend/dist

ENV PORT=8080
ENV FRONTEND_DIST=/app/frontend/dist
EXPOSE 8080
USER nobody
ENTRYPOINT ["/app/server"]
