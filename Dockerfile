# Multi-stage Dockerfile to build the Mattermost Authentik Plugin bundle
FROM node:22-alpine AS webapp-builder
WORKDIR /app/webapp
COPY webapp/package.json webapp/webpack.config.js ./
COPY webapp/src ./src
RUN npm install && npm run build

FROM golang:1.24-alpine AS server-builder
WORKDIR /app
RUN apk add --no-cache make git bash
COPY server ./server
COPY plugin.json Makefile ./
COPY --from=webapp-builder /app/webapp/dist ./webapp/dist
COPY assets ./assets
RUN make server
RUN make bundle

FROM scratch AS export
COPY --from=server-builder /app/dist/*.tar.gz /
