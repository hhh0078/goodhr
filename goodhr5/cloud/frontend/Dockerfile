FROM node:22-alpine
RUN yarn config set registry https://registry.npmmirror.com
WORKDIR /app
COPY package.json yarn.lock ./
RUN yarn install --frozen-lockfile
COPY . .
CMD ["yarn", "vite", "--host", "0.0.0.0"]
