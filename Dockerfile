# syntax=docker/dockerfile:1

# Stage 1: build frontend
FROM node:22-alpine AS frontend
WORKDIR /app/frontend
COPY frontend/package.json frontend/package-lock.json* ./
RUN npm ci
COPY frontend/ ./
RUN npm run build

# Stage 2: build backend
FROM golang:1.25-alpine AS backend
WORKDIR /app
COPY backend/go.mod backend/go.sum ./
RUN go mod download
COPY backend/ ./
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/markupmarkdown ./cmd/markupmarkdown

# Stage 3: runtime
FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata
WORKDIR /app
COPY --from=backend /out/markupmarkdown ./markupmarkdown
COPY backend/config ./config
COPY --from=frontend /app/frontend/dist ./web/dist

ENV MARKUPMARKDOWN_ENV=prod
ENV SERVER_HOST=0.0.0.0
ENV SERVER_PORT=4721
ENV FRONTEND_STATIC_DIR=/app/web/dist

EXPOSE 4721
CMD ["./markupmarkdown"]
