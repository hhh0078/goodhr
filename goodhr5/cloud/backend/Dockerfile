FROM golang:1.24-alpine
ENV GOPROXY=https://goproxy.cn,direct
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
CMD ["go", "run", "./cmd/server"]
