FROM node:22-alpine
RUN npm config set registry https://registry.npmmirror.com
WORKDIR /app
COPY package.json package-lock.json ./
RUN npm install --include=optional --no-package-lock
COPY . .
RUN npm run build
CMD ["npm", "run", "preview"]
