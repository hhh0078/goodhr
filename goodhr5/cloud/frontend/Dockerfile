FROM node:22-alpine
RUN npm config set registry https://registry.npmmirror.com
WORKDIR /app
COPY package.json package-lock.json ./
RUN npm install --include=optional --no-package-lock
COPY . .
CMD ["npm", "run", "dev", "--", "--host", "0.0.0.0"]
