# Stage 1: Build React + Vite + Tailwind Frontend
FROM node:22-alpine AS frontend-builder
WORKDIR /app/frontend
COPY frontend/package*.json ./
RUN npm ci
COPY frontend/ ./
RUN npm run build

# Stage 2: Build lightweight Go binary
FROM golang:1.25-alpine AS backend-builder
WORKDIR /app/backend
RUN apk add --no-cache git
COPY backend/go.mod backend/go.sum ./
RUN go mod download
COPY backend/ ./
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /out/runodoo ./cmd/runodoo

# Stage 3: Minimal Alpine Runtime (~25MB RAM)
FROM alpine:3.21
RUN apk add --no-cache ca-certificates git tzdata
WORKDIR /app
COPY --from=backend-builder /out/runodoo /app/runodoo
COPY --from=frontend-builder /app/frontend/dist /app/static

ENV RUNODOO_PORT=3000 \
    RUNODOO_DATA_DIR=/var/runodoo \
    RUNODOO_HOST_DATA_DIR=/var/runodoo \
    RUNODOO_STATIC_DIR=/app/static \
    DOCKER_SOCKET=/var/run/docker.sock

EXPOSE 3000
ENTRYPOINT ["/app/runodoo"]
