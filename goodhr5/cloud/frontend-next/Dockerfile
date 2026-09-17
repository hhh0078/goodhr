# 本文件负责构建并运行 GoodHR 新版 Next.js 前端。
FROM node:22-alpine AS dependencies
RUN npm config set registry https://registry.npmmirror.com
WORKDIR /app
COPY package.json package-lock.json ./
RUN npm ci

FROM node:22-alpine AS builder
WORKDIR /app
ARG NEXT_PUBLIC_CLOUD_API_BASE=https://goodhr5.58it.cn
ARG NEXT_PUBLIC_SITE_URL=https://goodhr5.58it.cn
ARG NEXT_PUBLIC_LEGACY_ADMIN_URL=/admin/
ARG CLOUD_API_BASE=https://goodhr5.58it.cn
ENV NEXT_PUBLIC_CLOUD_API_BASE=$NEXT_PUBLIC_CLOUD_API_BASE
ENV NEXT_PUBLIC_SITE_URL=$NEXT_PUBLIC_SITE_URL
ENV NEXT_PUBLIC_LEGACY_ADMIN_URL=$NEXT_PUBLIC_LEGACY_ADMIN_URL
ENV CLOUD_API_BASE=$CLOUD_API_BASE
COPY --from=dependencies /app/node_modules ./node_modules
COPY . .
RUN npm run build

FROM node:22-alpine AS runner
WORKDIR /app
ARG NEXT_PUBLIC_CLOUD_API_BASE=https://goodhr5.58it.cn
ARG NEXT_PUBLIC_SITE_URL=https://goodhr5.58it.cn
ARG NEXT_PUBLIC_LEGACY_ADMIN_URL=/admin/
ARG CLOUD_API_BASE=https://goodhr5.58it.cn
ENV NODE_ENV=production
ENV PORT=3000
ENV NEXT_PUBLIC_CLOUD_API_BASE=$NEXT_PUBLIC_CLOUD_API_BASE
ENV NEXT_PUBLIC_SITE_URL=$NEXT_PUBLIC_SITE_URL
ENV NEXT_PUBLIC_LEGACY_ADMIN_URL=$NEXT_PUBLIC_LEGACY_ADMIN_URL
ENV CLOUD_API_BASE=$CLOUD_API_BASE
COPY --from=builder /app/public ./public
COPY --from=builder /app/.next/standalone ./
COPY --from=builder /app/.next/static ./.next/static
EXPOSE 3000
CMD ["node", "server.js"]
