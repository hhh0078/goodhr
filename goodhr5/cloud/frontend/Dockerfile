FROM node:22-alpine
RUN npm config set registry https://registry.npmmirror.com
WORKDIR /app
COPY package.json package-lock.json ./
RUN npm install
COPY . .
CMD ["npx", "vite", "--host", "0.0.0.0"]
