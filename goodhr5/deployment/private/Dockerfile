# 本文件用干净官方镜像构建私有后端和绑定内网地址的 Chrome/Firefox 插件，不复用线上成品镜像。
FROM golang:1.24-alpine AS backend-builder
LABEL goodhr.project="goodhr" goodhr.build.stage="private-backend-builder"
ENV GOPROXY=https://goproxy.cn,direct
WORKDIR /src
COPY cloud/backend/go.mod cloud/backend/go.sum ./
RUN go mod download
COPY cloud/backend/ ./
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /goodhr-server ./cmd/server
LABEL goodhr.build.finished="true"

FROM alpine:3.22
RUN sed -i 's/dl-cdn.alpinelinux.org/mirrors.aliyun.com/g' /etc/apk/repositories \
    && apk add --no-cache ca-certificates tzdata
WORKDIR /app
COPY --from=backend-builder /goodhr-server /usr/local/bin/goodhr-server
COPY --from=backend-builder /src/db ./db
COPY --from=backend-builder /src/templates ./templates
COPY browser-extension/dist-private /opt/goodhr-extension
ENV GOODHR_DEPLOYMENT_MODE=private
ENV GOODHR_PRIVATE_ASSETS_DIR=/data/assets
ENV GOODHR_PRIVATE_EXTENSION_DIR=/opt/goodhr-extension
CMD ["goodhr-server"]
