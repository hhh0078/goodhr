# 本文件负责构建并运行 GoodHR 新版 Next.js 前端。
FROM node:22-alpine AS dependencies
LABEL goodhr.project="goodhr" goodhr.build.stage="frontend-dependencies"
RUN npm config set registry https://registry.npmmirror.com
WORKDIR /app
COPY package.json package-lock.json ./
RUN npm ci
LABEL goodhr.build.finished="true"

FROM node:22-alpine AS builder
LABEL goodhr.project="goodhr" goodhr.build.stage="frontend-builder"
WORKDIR /app
# 允许部署时限制构建堆内存和 Next.js 构建核数，参数不进入生产运行阶段。
ARG NODE_OPTIONS
ARG CIRCLE_NODE_TOTAL
ARG NEXT_PUBLIC_CLOUD_API_BASE=https://goodhr.58it.cn
ARG NEXT_PUBLIC_SITE_URL=https://goodhr.58it.cn
ARG NEXT_PUBLIC_LEGACY_ADMIN_URL=/admin/
ARG NEXT_PUBLIC_LOCAL_AGENT_PORT=""
ARG CLOUD_API_BASE=https://goodhr.58it.cn
ENV NEXT_PUBLIC_CLOUD_API_BASE=$NEXT_PUBLIC_CLOUD_API_BASE
ENV NEXT_PUBLIC_SITE_URL=$NEXT_PUBLIC_SITE_URL
ENV NEXT_PUBLIC_LEGACY_ADMIN_URL=$NEXT_PUBLIC_LEGACY_ADMIN_URL
ENV NEXT_PUBLIC_LOCAL_AGENT_PORT=$NEXT_PUBLIC_LOCAL_AGENT_PORT
ENV CLOUD_API_BASE=$CLOUD_API_BASE
COPY --from=dependencies /app/node_modules ./node_modules
COPY . .
RUN npm run build
LABEL goodhr.build.finished="true"

FROM node:22-alpine AS runner
WORKDIR /app
ARG NEXT_PUBLIC_CLOUD_API_BASE=https://goodhr.58it.cn
ARG NEXT_PUBLIC_SITE_URL=https://goodhr.58it.cn
ARG NEXT_PUBLIC_LEGACY_ADMIN_URL=/admin/
ARG NEXT_PUBLIC_LOCAL_AGENT_PORT=""
ARG CLOUD_API_BASE=https://goodhr.58it.cn
ENV NODE_ENV=production
ENV PORT=3000
ENV NEXT_PUBLIC_CLOUD_API_BASE=$NEXT_PUBLIC_CLOUD_API_BASE
ENV NEXT_PUBLIC_SITE_URL=$NEXT_PUBLIC_SITE_URL
ENV NEXT_PUBLIC_LEGACY_ADMIN_URL=$NEXT_PUBLIC_LEGACY_ADMIN_URL
ENV NEXT_PUBLIC_LOCAL_AGENT_PORT=$NEXT_PUBLIC_LOCAL_AGENT_PORT
ENV CLOUD_API_BASE=$CLOUD_API_BASE
COPY --from=builder /app/public ./public
COPY --from=builder /app/.next/standalone ./
COPY --from=builder /app/.next/static ./.next/static
EXPOSE 3000
CMD ["node", "server.js"]
