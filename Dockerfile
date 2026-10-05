FROM --platform=$BUILDPLATFORM node:22-alpine AS webui
WORKDIR /webui
ARG NPM_REGISTRY=https://registry.npmmirror.com
COPY webui/package.json webui/package-lock.json ./
RUN npm ci --registry="$NPM_REGISTRY" --no-audit --no-fund
COPY webui/ ./
RUN npm run build

FROM --platform=$BUILDPLATFORM golang:1.24-alpine AS build
ARG TARGETOS
ARG TARGETARCH
WORKDIR /src
ENV CGO_ENABLED=0
ARG GOPROXY=https://goproxy.cn,direct
ENV GOPROXY=$GOPROXY
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=webui /webui/dist ./webui/dist
# $BUILDPLATFORM 上交叉编译：CGO_ENABLED=0 时 Go 不需要目标平台的工具链。
# 变量为空说明不是 buildx 构建，回退到本机架构。
RUN GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH:-$(go env GOARCH)} \
    go build -trimpath -ldflags="-s -w" -o /out/qb-guard .

FROM alpine:3.20
RUN apk add --no-cache ca-certificates
WORKDIR /
COPY --from=build /out/qb-guard /qb-guard
# 配置、脚本、订阅缓存与状态都从挂载点读取，默认 /config.yaml 与 /data
ENTRYPOINT ["/qb-guard", "/config.yaml"]
