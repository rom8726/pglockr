# Multi-stage build: compile the UI, embed it, produce a static Go binary.
FROM node:22-alpine AS ui
WORKDIR /app/web
COPY web/package.json web/package-lock.json* ./
RUN npm install
COPY web/ ./
RUN npm run build

FROM golang:1.26-alpine AS build
ARG VERSION=dev
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=ui /app/web/dist ./web/dist
RUN CGO_ENABLED=0 go build -ldflags "-s -w -X main.version=${VERSION}" -o /pglockr ./cmd/pglockr

FROM gcr.io/distroless/static-debian12
COPY --from=build /pglockr /pglockr
EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["/pglockr"]
